package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/footprintai/containarium/internal/sshkey"
	"github.com/spf13/cobra"
)

// `containarium connect <box>` turns the token the caller already holds
// into a shell, without the user ever hand-managing an SSH key. It
// resolves the box, authorizes a managed key on the caller's behalf, and
// then either drops into an interactive session (default), runs one
// command and returns its output (--exec, the agent-native path), or just
// prints the ready ssh invocation (--print).
//
// CLI-first per CLAUDE.md: this is the canonical surface; an MCP `connect`
// tool is a thin wrapper over the same resolve+authorize core.
//
// It is glue over primitives the daemon already serves:
//   - GET  /v1/containers/{box}            → ssh_host, username, state, IP
//   - POST /v1/containers/{box}/ssh-keys   → authorize a key (idempotent)
//
// The SSH target is `username@host` where host prefers Container.ssh_host
// (FootprintAI/Containarium#452) and falls back to the container IP — so
// connect works against both sentinel-fronted and direct deployments.
var (
	connectServer   string
	connectExec     string
	connectPrint    bool
	connectKeyPath  string
	connectIdentity string
	connectUser     string
	connectHost     string
	connectPort     int
)

var connectCmd = &cobra.Command{
	Use:   "connect <box>",
	Short: "Open a shell on a box using your token (no SSH-key setup)",
	Long: `Connect to one of your boxes over SSH using the token you're already
logged in with — connect authorizes a managed key for you, so you never
have to set up or copy an SSH key yourself.

Three modes:

  containarium connect my-box                 # interactive shell
  containarium connect my-box --exec "make"   # run one command, return its output
  containarium connect my-box --print         # authorize + print the ssh command, don't connect

The SSH target is the box's ssh_host (or its IP if the daemon reports no
ssh_host) and its SSH username. Override either with --host / --user.`,
	Args: cobra.ExactArgs(1),
	RunE: runConnect,
}

func init() {
	connectCmd.Flags().StringVar(&connectServer, "server", "", "server to talk to (default: your logged-in server)")
	connectCmd.Flags().StringVar(&connectExec, "exec", "", "run one command over SSH and return its output (non-interactive)")
	connectCmd.Flags().BoolVar(&connectPrint, "print", false, "authorize the key and print the ready ssh command; do not connect")
	connectCmd.Flags().StringVar(&connectKeyPath, "key", "", "public key to authorize (default: reuse or generate the managed key)")
	connectCmd.Flags().StringVar(&connectIdentity, "identity", "", "private key to authenticate with (default: derived from the public key)")
	connectCmd.Flags().StringVar(&connectUser, "user", "", "override the SSH login user (default: the box's SSH username)")
	connectCmd.Flags().StringVar(&connectHost, "host", "", "override the SSH host (default: the box's ssh_host, else its IP)")
	connectCmd.Flags().IntVar(&connectPort, "port", 22, "SSH port")
	rootCmd.AddCommand(connectCmd)
}

// ---- wire DTOs (grpc-gateway emits camelCase; UseProtoNames=false) -----

type connectContainer struct {
	Username string `json:"username"`
	State    string `json:"state"`
	SshHost  string `json:"sshHost"`
	Network  struct {
		IpAddress string `json:"ipAddress"`
	} `json:"network"`
}

type getContainerResp struct {
	Container connectContainer `json:"container"`
}

// AddSSHKeyRequest body. The {username} path segment carries the box; the
// body only needs the key. Body accepts camelCase or snake_case on the
// wire — we send camelCase to match the daemon's emit convention.
type authorizeKeyReq struct {
	SshPublicKey string `json:"sshPublicKey"`
}

// ---- thin REST client (deliberately not ssh.go's sshHTTPClient: that
// one maps 404→Unimplemented, which would mask "box not found") ---------

type connectAPI struct {
	hc     *http.Client
	server string
	token  string
}

func newConnectAPI(server string) (*connectAPI, error) {
	tok := resolveAuthToken(server)
	if tok == "" {
		return nil, fmt.Errorf("no auth token for %s — run `containarium login` first", server)
	}
	return &connectAPI{
		hc:     &http.Client{Timeout: 30 * time.Second},
		server: server,
		token:  tok,
	}, nil
}

// do sends the request and returns the HTTP status so callers can
// distinguish 404 (box not found) from other failures. out is decoded on
// a 2xx response when non-nil.
func (c *connectAPI) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("marshal request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.server+path, rdr)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(rb) > 0 {
			if err := json.Unmarshal(rb, out); err != nil {
				return resp.StatusCode, fmt.Errorf("decode response: %w", err)
			}
		}
		return resp.StatusCode, nil
	}
	return resp.StatusCode, fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(rb)))
}

func (c *connectAPI) GetContainer(ctx context.Context, box string) (*connectContainer, error) {
	var out getContainerResp
	st, err := c.do(ctx, http.MethodGet, "/v1/containers/"+url.PathEscape(box), nil, &out)
	if st == http.StatusNotFound {
		return nil, fmt.Errorf("box %q not found (check the name, or `containarium list`)", box)
	}
	if err != nil {
		return nil, err
	}
	return &out.Container, nil
}

func (c *connectAPI) AuthorizeKey(ctx context.Context, box, pub string) error {
	_, err := c.do(ctx, http.MethodPost,
		"/v1/containers/"+url.PathEscape(box)+"/ssh-keys",
		authorizeKeyReq{SshPublicKey: pub}, nil)
	return err
}

// ---- pure resolution (unit-tested without network/exec) ----------------

type connectTarget struct {
	User string
	Host string
	Port int
}

// containerIsRunning reports whether a box can accept connections.
// protojson emits the enum identifier ("CONTAINER_STATE_RUNNING");
// we also accept the friendly "Running" defensively.
func containerIsRunning(state string) bool {
	return state == "CONTAINER_STATE_RUNNING" || strings.EqualFold(state, "running")
}

// prettyState trims the proto enum prefix for human-facing messages.
func prettyState(state string) string {
	s := strings.TrimPrefix(state, "CONTAINER_STATE_")
	if s == "" {
		return "unknown"
	}
	return strings.ToLower(s)
}

// buildConnectTarget derives user@host:port, preferring overrides, then
// the daemon's ssh_host, then the container IP. Returns an actionable
// error when neither a host nor an IP is known yet (box still placing).
func buildConnectTarget(c *connectContainer, userOverride, hostOverride string, port int) (connectTarget, error) {
	user := userOverride
	if user == "" {
		user = c.Username
	}
	if user == "" {
		return connectTarget{}, fmt.Errorf("could not determine the SSH user for this box — pass --user")
	}
	host := hostOverride
	if host == "" {
		host = c.SshHost
	}
	if host == "" {
		host = c.Network.IpAddress
	}
	if host == "" {
		return connectTarget{}, fmt.Errorf("box has no ssh_host or IP yet (still being placed?) — retry shortly, or pass --host")
	}
	if port == 0 {
		port = 22
	}
	return connectTarget{User: user, Host: host, Port: port}, nil
}

// buildSSHArgs assembles the ssh argv. A managed key is pinned with
// IdentitiesOnly so ssh-agent's other keys don't shadow it; accept-new
// avoids a first-connect prompt (important for --exec / agents) without
// the blanket insecurity of StrictHostKeyChecking=no.
func buildSSHArgs(t connectTarget, identity, execCmd string) []string {
	args := []string{
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if identity != "" {
		args = append(args, "-i", identity)
	}
	if t.Port != 0 && t.Port != 22 {
		args = append(args, "-p", strconv.Itoa(t.Port))
	}
	args = append(args, t.User+"@"+t.Host)
	if execCmd != "" {
		// One command string, run by the box's login shell.
		args = append(args, execCmd)
	}
	return args
}

// ---- command handler ---------------------------------------------------

// obtainConnectKey returns the public key to authorize and the private
// key path to authenticate with. With --key it uses the supplied public
// key; otherwise it reuses (or generates once) the managed key the
// `ssh setup` flow already uses, so the user never hand-manages a key.
// The private path defaults to the public path minus ".pub" unless
// --identity overrides it.
func obtainConnectKey() (pub, privPath string, err error) {
	var pubPath string
	if connectKeyPath != "" {
		pub, err = sshkey.ReadPublicKey(connectKeyPath)
		if err != nil {
			return "", "", err
		}
		pubPath = connectKeyPath
	} else {
		pubPath, pub, _, err = sshkey.LocateOrGenerate(sshkey.LocateOpts{})
		if err != nil {
			return "", "", fmt.Errorf("locate or generate managed key: %w", err)
		}
	}
	privPath = connectIdentity
	if privPath == "" {
		privPath = strings.TrimSuffix(pubPath, ".pub")
	}
	return pub, privPath, nil
}

func runConnect(cmd *cobra.Command, args []string) error {
	box := args[0]
	if err := validateBoxName(box); err != nil {
		return err
	}
	// Diagnostics go to stderr so --exec keeps stdout clean for command
	// output (an agent parsing --exec output gets only the command's bytes).
	diag := cmd.ErrOrStderr()

	server := pickSSHServer(connectServer)
	api, err := newConnectAPI(server)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	c, err := api.GetContainer(ctx, box)
	if err != nil {
		return err
	}
	if !containerIsRunning(c.State) {
		return fmt.Errorf("box %q is %s, not running — start it first (`containarium start %s`)",
			box, prettyState(c.State), box)
	}
	target, err := buildConnectTarget(c, connectUser, connectHost, connectPort)
	if err != nil {
		return err
	}

	pub, privPath, err := obtainConnectKey()
	if err != nil {
		return err
	}
	if err := api.AuthorizeKey(ctx, box, pub); err != nil {
		return fmt.Errorf("authorize key on %q: %w", box, err)
	}
	fp, _ := sshkey.Fingerprint(pub)
	fmt.Fprintf(diag, "✓ %s → %s@%s (authorized %s)\n", box, target.User, target.Host, fp)

	sshArgs := buildSSHArgs(target, privPath, connectExec)
	if connectPrint {
		// The ready invocation is the deliverable here → stdout.
		fmt.Fprintf(cmd.OutOrStdout(), "ssh %s\n", strings.Join(sshArgs, " "))
		return nil
	}
	return runSSH(sshArgs)
}

// runSSH execs the local ssh client, inheriting the terminal. For an
// interactive session stdin is the user's TTY (ssh allocates a PTY); for
// --exec it's a one-shot command whose stdout/stderr stream through. The
// remote exit code is propagated verbatim so CI / agents see failures.
func runSSH(args []string) error {
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh not found in PATH: %w", err)
	}
	c := exec.Command(sshBin, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		return fmt.Errorf("ssh: %w", err)
	}
	return nil
}
