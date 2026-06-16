//go:build k8s

package k8s

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// generateHostKey makes an ed25519 host key for a box. It returns the private
// key in OpenSSH PEM form (what the box's entrypoint feeds dropbearconvert) and
// the public key in authorized-key form ("ssh-ed25519 AAAA…", no comment).
func generateHostKey() (privPEM []byte, pubAuthorized string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, "", err
	}
	pubAuthorized = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	return pem.EncodeToMemory(block), pubAuthorized, nil
}

// knownHostsData builds sshpiper's spec.to.known_hosts_data: a base64-encoded
// known_hosts line pinning the box's host key. The host is the upstream
// "host:port"; known_hosts brackets a non-22 port host as "[host]:port".
//
//	hostPort = "box-0.boxes.tenant-a.svc.cluster.local:2222"
//	pubAuthorized = "ssh-ed25519 AAAA..."
//	→ base64("[box-0.boxes.tenant-a.svc.cluster.local]:2222 ssh-ed25519 AAAA...")
func knownHostsData(hostPort, pubAuthorized string) string {
	host, port, found := strings.Cut(hostPort, ":")
	pattern := host
	if found {
		pattern = fmt.Sprintf("[%s]:%s", host, port)
	}
	line := pattern + " " + pubAuthorized
	return base64.StdEncoding.EncodeToString([]byte(line))
}
