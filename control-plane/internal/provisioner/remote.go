package provisioner

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Remote describes a Docker host the control plane drives from afar: a
// region server. The Engine API is reached over SSH (the daemon's unix
// socket through the SSH connection, like `docker -H ssh://`) or over TCP
// with TLS client certificates (`dockerd --tlsverify`).
type Remote struct {
	Transport string // ssh or tls
	// Endpoint is ssh://user@host[:port] or tcp://host:port.
	Endpoint string
	// SSH: the private key, and the server's host key in authorized_keys
	// form. An empty host key is accepted on first connect and reported
	// through HostKey, to be stored and required from then on.
	SSHKey     string
	SSHHostKey string
	// TLS: the CA that signed the daemon's certificate, and the client's
	// certificate and key, all PEM.
	TLSCA, TLSCert, TLSKey string
	// PublicHost is the name customers' database connections use.
	PublicHost string
	// Relay is how the control plane and gateway reach project services.
	Relay RelayConfig
}

// dockerSocket is where the daemon listens on a region server.
const dockerSocket = "/var/run/docker.sock"

// NewRemoteDocker returns a driver for a region server. It does not connect
// until the first call.
func NewRemoteDocker(r Remote, offsite *Offsite) (*Docker, error) {
	u, err := url.Parse(r.Endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("the endpoint must look like ssh://user@host or tcp://host:2376")
	}
	var dial func(ctx context.Context) (net.Conn, error)
	var hostKey func() string
	switch r.Transport {
	case "ssh":
		if u.Scheme != "ssh" {
			return nil, fmt.Errorf("an SSH endpoint starts with ssh://")
		}
		t, err := newSSHTunnel(u, r.SSHKey, r.SSHHostKey)
		if err != nil {
			return nil, err
		}
		dial, hostKey = t.dial, t.hostKey
	case "tls":
		if u.Scheme != "tcp" && u.Scheme != "https" {
			return nil, fmt.Errorf("a TLS endpoint starts with tcp://")
		}
		cfg, err := clientTLS(u.Hostname(), r.TLSCA, r.TLSCert, r.TLSKey)
		if err != nil {
			return nil, err
		}
		addr := u.Host
		if u.Port() == "" {
			addr = net.JoinHostPort(u.Hostname(), "2376")
		}
		dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: cfg}
		dial = func(ctx context.Context) (net.Conn, error) { return dialer.DialContext(ctx, "tcp", addr) }
	default:
		return nil, fmt.Errorf("transport must be ssh or tls")
	}
	if r.PublicHost == "" {
		return nil, fmt.Errorf("a public host name is needed for database connections")
	}
	relay, err := NewRelay(r.Relay, r.PublicHost)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		DialContext:     func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
		IdleConnTimeout: 90 * time.Second,
	}
	return &Docker{
		http:       &http.Client{Transport: transport},
		publicHost: r.PublicHost,
		// The relay joins every project network, as the control plane and
		// gateway do on the local host.
		attach:  []string{relayName},
		offsite: offsite,
		relay:   relay,
		hostKey: hostKey,
	}, nil
}

// HostKey is the SSH host key seen on the last connection, in
// authorized_keys form; empty for TLS and before connecting.
func (d *Docker) HostKey() string {
	if d.hostKey == nil {
		return ""
	}
	return d.hostKey()
}

func clientTLS(serverName, caPEM, certPEM, keyPEM string) (*tls.Config, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, fmt.Errorf("the CA certificate is not valid PEM")
	}
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("the client certificate and key do not form a pair: %w", err)
	}
	return &tls.Config{ServerName: serverName, RootCAs: pool, Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}

// sshTunnel keeps one SSH connection to a region server and opens a stream
// to the Docker socket over it per HTTP connection.
type sshTunnel struct {
	addr   string
	config *ssh.ClientConfig
	mu     sync.Mutex
	client *ssh.Client
	seen   string // host key of the last connection
}

func newSSHTunnel(u *url.URL, keyPEM, hostKey string) (*sshTunnel, error) {
	signer, err := ssh.ParsePrivateKey([]byte(keyPEM))
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, fmt.Errorf("the SSH key has a passphrase; use a key without one for FloBase")
		}
		return nil, fmt.Errorf("the SSH private key is not valid: %w", err)
	}
	user := u.User.Username()
	if user == "" {
		user = "root"
	}
	addr := u.Host
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "22")
	}
	t := &sshTunnel{addr: addr}
	var expected ssh.PublicKey
	if strings.TrimSpace(hostKey) != "" {
		if expected, err = ParseHostKey(hostKey); err != nil {
			return nil, err
		}
	}
	t.config = &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if expected != nil && string(key.Marshal()) != string(expected.Marshal()) {
				return fmt.Errorf("the server's SSH host key changed (now %s); if that is expected, remove and re-add the region server", ssh.FingerprintSHA256(key))
			}
			t.mu.Lock()
			t.seen = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
			t.mu.Unlock()
			return nil
		},
		Timeout: 10 * time.Second,
	}
	if expected != nil {
		// Ask for the pinned key's type; the server may hold others too.
		t.config.HostKeyAlgorithms = []string{expected.Type()}
		if expected.Type() == ssh.KeyAlgoRSA {
			t.config.HostKeyAlgorithms = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
		}
	}
	return t, nil
}

// ParseHostKey reads an SSH host key as `ssh-ed25519 AAAA…` or as a line
// of ssh-keyscan or known_hosts output.
func ParseHostKey(text string) (ssh.PublicKey, error) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line)); err == nil {
			return key, nil
		}
		if _, _, key, _, _, err := ssh.ParseKnownHosts([]byte(line)); err == nil {
			return key, nil
		}
	}
	return nil, fmt.Errorf("the SSH host key is not valid; it looks like `ssh-ed25519 AAAA…` (ssh-keyscan output works too)")
}

func (t *sshTunnel) hostKey() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seen
}

func (t *sshTunnel) connect() (*ssh.Client, error) {
	t.mu.Lock()
	c := t.client
	t.mu.Unlock()
	if c != nil {
		return c, nil
	}
	c, err := ssh.Dial("tcp", t.addr, t.config)
	if err != nil {
		return nil, fmt.Errorf("ssh %s: %w", t.addr, err)
	}
	t.mu.Lock()
	if t.client != nil {
		// Another call connected first; use its connection.
		c.Close()
		c = t.client
	} else {
		t.client = c
		go func() {
			_ = c.Wait()
			t.mu.Lock()
			if t.client == c {
				t.client = nil
			}
			t.mu.Unlock()
		}()
	}
	t.mu.Unlock()
	return c, nil
}

func (t *sshTunnel) dial(ctx context.Context) (net.Conn, error) {
	for attempt := 0; ; attempt++ {
		c, err := t.connect()
		if err != nil {
			return nil, err
		}
		conn, err := c.Dial("unix", dockerSocket)
		if err == nil {
			return conn, nil
		}
		// A dropped connection is replaced once; other errors are real.
		c.Close()
		t.mu.Lock()
		if t.client == c {
			t.client = nil
		}
		t.mu.Unlock()
		if attempt > 0 || ctx.Err() != nil {
			return nil, fmt.Errorf("reach the Docker socket over SSH: %w", err)
		}
	}
}
