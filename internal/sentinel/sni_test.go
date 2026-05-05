package sentinel

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractSNI_RealClientHello generates a real TLS ClientHello via the
// stdlib's tls.Client and verifies extractSNI parses the SNI field correctly.
func TestExtractSNI_RealClientHello(t *testing.T) {
	cases := []string{
		"containarium-prod.kafeido.app",
		"a.b",            // minimal
		"x.example.com",  // typical
	}
	for _, sni := range cases {
		t.Run(sni, func(t *testing.T) {
			// Pipe a stdlib TLS handshake into our parser.
			clientConn, serverConn := net.Pipe()
			defer clientConn.Close()
			defer serverConn.Close()

			go func() {
				cfg := &tls.Config{ServerName: sni, InsecureSkipVerify: true}
				_ = tls.Client(clientConn, cfg).Handshake() // will fail; we just want bytes
			}()

			br := bufio.NewReaderSize(serverConn, 16389)
			hdr, err := br.Peek(5)
			require.NoError(t, err)
			recLen := int(hdr[3])<<8 | int(hdr[4])
			full, err := br.Peek(5 + recLen)
			require.NoError(t, err)

			got, err := extractSNI(full)
			require.NoError(t, err)
			assert.Equal(t, sni, got)
		})
	}
}

func TestExtractSNI_Errors(t *testing.T) {
	tests := []struct {
		name string
		buf  []byte
	}{
		{"empty", nil},
		{"too short for record header", []byte{0x16, 0x03}},
		{"wrong content type", []byte{0x17, 0x03, 0x03, 0x00, 0x00}},
		{"record body truncated", []byte{0x16, 0x03, 0x03, 0x00, 0x10, 0x01}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := extractSNI(tc.buf)
			assert.Error(t, err)
		})
	}
}

// TestSNIRouting_DispatchToPrimaryOrFallback wires up a fake "primary" TLS
// server and a fake "fallback" TLS server, then drives connections through
// the sentinel's SNI-routing handler. SNI-matched connections (by primary
// hostname or alias) land on the primary; unknown SNIs fall back.
func TestSNIRouting_DispatchToPrimaryOrFallback(t *testing.T) {
	primaryAddr, primaryHits := startEchoListener(t, "PRIMARY")
	fallbackAddr, fallbackHits := startEchoListener(t, "FALLBACK")

	m := &Manager{primaries: NewPrimaryRegistry()}
	primaryHost, primaryPortStr, err := net.SplitHostPort(primaryAddr)
	require.NoError(t, err)
	m.primaries.Register(Primary{
		Pool:     "prod",
		Hostname: "pool-prod.example",
		Aliases:  []string{"app.example"}, // app domain handled by the same primary
		IP:       primaryHost,
		Port:     mustAtoi(t, primaryPortStr),
	})
	handler := m.buildSNIRoutingHandler(fallbackAddr)

	// SNI=pool-prod.example (primary hostname) → primary
	got := dialThroughHandler(t, handler, &tls.Config{
		ServerName: "pool-prod.example", InsecureSkipVerify: true,
	})
	assert.Equal(t, "PRIMARY", got, "primary hostname should land on primary")

	// SNI=app.example (alias) → primary
	got = dialThroughHandler(t, handler, &tls.Config{
		ServerName: "app.example", InsecureSkipVerify: true,
	})
	assert.Equal(t, "PRIMARY", got, "aliased hostname should land on the same primary")

	// SNI not in registry → fallback
	got = dialThroughHandler(t, handler, &tls.Config{
		ServerName: "stranger.example", InsecureSkipVerify: true,
	})
	assert.Equal(t, "FALLBACK", got, "unknown SNI should land on fallback")

	assert.Equal(t, 2, primaryHits(), "primary hostname + alias should both hit the primary")
	assert.Equal(t, 1, fallbackHits())
}

// dialThroughHandler simulates an inbound TLS connection by accepting on a
// throwaway listener and handing the server side to the SNI handler under
// test, while the test goroutine drives the client side.
func dialThroughHandler(t *testing.T, handler func(net.Conn), clientCfg *tls.Config) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		serverConn, err := ln.Accept()
		if err != nil {
			return
		}
		handler(serverConn)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	tlsConn := tls.Client(conn, clientCfg)
	require.NoError(t, tlsConn.Handshake())
	defer tlsConn.Close()

	_, err = tlsConn.Write([]byte("ping\n"))
	require.NoError(t, err)
	buf := make([]byte, 64)
	_ = tlsConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := tlsConn.Read(buf)
	return string(buf[:findFirst(buf[:n], '\n')])
}

// startEchoListener starts a TLS server that responds to any read with `tag`
// followed by '\n', regardless of input.
func startEchoListener(t *testing.T, tag string) (addr string, hits func() int) {
	t.Helper()

	cert, key := genSelfSigned(t, "any")
	tlsCert, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)
	cfg := &tls.Config{Certificates: []tls.Certificate{tlsCert}}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	var (
		mu      sync.Mutex
		hitsVal int
	)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			hitsVal++
			mu.Unlock()
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.ReadFull(c, make([]byte, 5))
				_, _ = c.Write([]byte(tag + "\n"))
			}(conn)
		}
	}()
	return ln.Addr().String(), func() int {
		mu.Lock()
		defer mu.Unlock()
		return hitsVal
	}
}

func genSelfSigned(t *testing.T, cn string) (cert, key []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{cn, "*.example", "any"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)
	cert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDer, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	key = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer})
	return
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	for _, ch := range s {
		require.True(t, ch >= '0' && ch <= '9', "non-numeric port")
		n = n*10 + int(ch-'0')
	}
	return n
}

func findFirst(b []byte, ch byte) int {
	for i, c := range b {
		if c == ch {
			return i
		}
	}
	return len(b)
}
