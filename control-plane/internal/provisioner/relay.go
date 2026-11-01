package provisioner

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The relay is how the control plane and gateway reach a region server's
// project services, which sit on Docker networks of another machine. It is
// a stock nginx container on the region server, joined to every project
// network like the control plane is locally, that accepts TLS only from
// holders of the relay's client certificate and forwards each connection to
// the service named in its TLS server name ("flobase-<ref>-rest.3000.relay").
// One published port carries REST, auth and database connections alike, and
// nothing else on the server needs to be exposed besides customers'
// database ports.
//
// Both certificates are self-signed and made by the control plane when the
// server is added: the control plane pins the relay's certificate exactly,
// and the relay trusts exactly the control plane's.

const (
	relayName  = "flobase-relay"
	relayImage = "nginx:1.27-alpine"
	// relayListen is the port inside the relay container.
	relayListen = 8443
	// DefaultRelayPort is where region servers publish the relay. It is an
	// uncommon port, so it does not collide with what else runs there.
	DefaultRelayPort = 28474
)

// RelayConfig is one region server's relay: its published port and the two
// certificate pairs, PEM.
type RelayConfig struct {
	Port       int
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

// NewRelayCerts makes the relay's server and client certificates.
func NewRelayCerts(port int) (RelayConfig, error) {
	serverCert, serverKey, err := selfSigned("flobase relay", x509.ExtKeyUsageServerAuth)
	if err != nil {
		return RelayConfig{}, err
	}
	clientCert, clientKey, err := selfSigned("flobase control plane", x509.ExtKeyUsageClientAuth)
	if err != nil {
		return RelayConfig{}, err
	}
	return RelayConfig{Port: port, ServerCert: serverCert, ServerKey: serverKey, ClientCert: clientCert, ClientKey: clientKey}, nil
}

func selfSigned(name string, usage x509.ExtKeyUsage) (certPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return "", "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{"relay"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(20, 0, 0),
		// A self-signed certificate is its own CA, which is how the relay
		// uses the client certificate as its only trusted one.
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})), nil
}

// Relay connects to project services through a region server's relay.
type Relay struct {
	addr string
	tls  *tls.Config
	mu   sync.Mutex
	// forwards are local listeners standing in for one service each, for
	// clients that take a host and port rather than a dialer.
	forwards map[string]string
}

// NewRelay returns a client for the relay published on host.
func NewRelay(c RelayConfig, host string) (*Relay, error) {
	block, _ := pem.Decode([]byte(c.ServerCert))
	if block == nil {
		return nil, errors.New("the relay certificate is not valid PEM")
	}
	pinned := block.Bytes
	client, err := tls.X509KeyPair([]byte(c.ClientCert), []byte(c.ClientKey))
	if err != nil {
		return nil, fmt.Errorf("relay client certificate: %w", err)
	}
	port := c.Port
	if port == 0 {
		port = DefaultRelayPort
	}
	return &Relay{
		addr: net.JoinHostPort(host, strconv.Itoa(port)),
		tls: &tls.Config{
			Certificates: []tls.Certificate{client},
			MinVersion:   tls.VersionTLS13,
			// The relay's certificate is pinned instead of checked against
			// a CA and host name: only that exact certificate is accepted.
			InsecureSkipVerify: true,
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				if len(raw) == 0 || !bytes.Equal(raw[0], pinned) {
					return errors.New("the relay presented a certificate other than its own")
				}
				return nil
			},
		},
		forwards: map[string]string{},
	}, nil
}

// DialContext connects to addr ("flobase-<ref>-<service>:<port>") on the
// region server. Its signature fits http.Transport.
func (r *Relay) DialContext(ctx context.Context, _, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || !strings.HasPrefix(host, "flobase-") {
		return nil, fmt.Errorf("the relay only reaches project services, not %q", addr)
	}
	cfg := r.tls.Clone()
	cfg.ServerName = host + "." + port + ".relay"
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: cfg}
	conn, err := d.DialContext(ctx, "tcp", r.addr)
	if err != nil {
		return nil, fmt.Errorf("relay %s: %w", r.addr, err)
	}
	return conn, nil
}

// Check makes a TLS connection to the relay, proving it runs and holds
// the expected certificate.
func (r *Relay) Check(ctx context.Context) error {
	conn, err := r.DialContext(ctx, "tcp", relayName+"-check:1")
	if err != nil {
		return err
	}
	// The handshake finishes on first use; a read surfaces its errors. The
	// relay then closes the connection, as nothing listens on the target.
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	_, err = conn.Read(make([]byte, 1))
	conn.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil
		}
		if strings.Contains(err.Error(), "certificate") || strings.Contains(err.Error(), "handshake") {
			return fmt.Errorf("relay %s: %w", r.addr, err)
		}
	}
	return nil
}

// Forward returns a local address that reaches target ("flobase-<ref>-db:5432")
// through the relay, starting a listener for it on first use.
func (r *Relay) Forward(target string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if addr, ok := r.forwards[target]; ok {
		host, port, _ := net.SplitHostPort(addr)
		p, _ := strconv.Atoi(port)
		return host, p, nil
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", 0, err
	}
	r.forwards[target] = l.Addr().String()
	go func() {
		for {
			local, err := l.Accept()
			if err != nil {
				return
			}
			go r.pipe(local, target)
		}
	}()
	return "127.0.0.1", l.Addr().(*net.TCPAddr).Port, nil
}

func (r *Relay) pipe(local net.Conn, target string) {
	defer local.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	remote, err := r.DialContext(ctx, "tcp", target)
	cancel()
	if err != nil {
		return
	}
	defer remote.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(remote, local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(local, remote); done <- struct{}{} }()
	<-done
}

// relayConf is nginx's whole configuration: TCP forwarding only, picking
// the upstream from the TLS server name. Upstream names resolve through
// Docker's DNS on the networks the relay has joined.
const relayConf = `worker_processes 1;
error_log /dev/stderr warn;
events { worker_connections 4096; }
stream {
  resolver 127.0.0.11 valid=10s ipv6=off;
  map $ssl_server_name $relay_upstream {
    "~^(?<svc>flobase-[a-z0-9]+-[a-z]+)\.(?<port>[0-9]+)\.relay$" $svc:$port;
    default 127.0.0.1:9;
  }
  server {
    listen 8443 ssl;
    ssl_protocols TLSv1.3;
    ssl_certificate /etc/nginx/relay/server.crt;
    ssl_certificate_key /etc/nginx/relay/server.key;
    ssl_client_certificate /etc/nginx/relay/client.crt;
    ssl_verify_client on;
    proxy_connect_timeout 10s;
    proxy_timeout 1h;
    proxy_pass $relay_upstream;
  }
}
`

// relayScript writes the configuration and certificates from the
// environment, then runs nginx; the image stays a stock one.
const relayScript = `set -e
mkdir -p /etc/nginx/relay
printf '%s' "$RELAY_CONF" > /etc/nginx/nginx.conf
printf '%s' "$RELAY_SERVER_CERT" > /etc/nginx/relay/server.crt
printf '%s' "$RELAY_SERVER_KEY" > /etc/nginx/relay/server.key
printf '%s' "$RELAY_CLIENT_CERT" > /etc/nginx/relay/client.crt
chmod 600 /etc/nginx/relay/server.key
exec nginx -g 'daemon off;'`

// EnsureRelay starts the relay on a region server, or replaces it when its
// configuration changed, and joins it to every project network there.
func (d *Docker) EnsureRelay(ctx context.Context, c RelayConfig) error {
	env := []string{
		"RELAY_CONF=" + relayConf, "RELAY_SERVER_CERT=" + c.ServerCert, "RELAY_SERVER_KEY=" + c.ServerKey,
		"RELAY_CLIENT_CERT=" + c.ClientCert,
	}
	sum := sha256.Sum256([]byte(strings.Join(env, "\n") + relayImage + strconv.Itoa(c.Port)))
	hash := hex.EncodeToString(sum[:8])
	var current struct {
		State  struct{ Running bool }
		Config struct{ Labels map[string]string }
	}
	err := d.call(ctx, "GET", "/containers/"+relayName+"/json", nil, &current)
	if err == nil && current.State.Running && current.Config.Labels["dev.flobase.relay"] == hash {
		return d.attachRelay(ctx)
	}
	if err == nil {
		if err := d.call(ctx, "DELETE", "/containers/"+relayName+"?force=true", nil, nil); err != nil && !isNotFound(err) {
			return fmt.Errorf("replace relay: %w", err)
		}
	} else if !isNotFound(err) {
		return err
	}
	// Its own network gives the relay Docker's DNS, which the default
	// bridge lacks.
	if err := d.call(ctx, "POST", "/networks/create", map[string]any{"Name": relayName}, nil); err != nil &&
		!strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("create relay network: %w", err)
	}
	if err := d.pull(ctx, relayImage); err != nil {
		return fmt.Errorf("pull %s: %w", relayImage, err)
	}
	port := strconv.Itoa(relayListen) + "/tcp"
	config := map[string]any{
		"Image": relayImage, "Entrypoint": []string{"sh", "-c", relayScript}, "Env": env,
		"Labels":       map[string]string{"dev.flobase.relay": hash},
		"ExposedPorts": map[string]any{port: map[string]any{}},
		"HostConfig": map[string]any{
			"NetworkMode":   relayName,
			"RestartPolicy": map[string]any{"Name": "unless-stopped"},
			"Memory":        int64(128) * 1024 * 1024,
			"PortBindings":  map[string]any{port: []map[string]string{{"HostPort": strconv.Itoa(c.Port)}}},
		},
	}
	if err := d.call(ctx, "POST", "/containers/create?name="+relayName, config, nil); err != nil {
		return fmt.Errorf("create relay: %w", err)
	}
	if err := d.call(ctx, "POST", "/containers/"+relayName+"/start", nil, nil); err != nil {
		return fmt.Errorf("start relay: %w", err)
	}
	return d.attachRelay(ctx)
}

// attachRelay joins the relay to the networks of the projects on this
// server.
func (d *Docker) attachRelay(ctx context.Context) error {
	filter := `{"label":["` + label + `"]}`
	var networks []struct{ Labels map[string]string }
	if err := d.call(ctx, "GET", "/networks?filters="+url.QueryEscape(filter), nil, &networks); err != nil {
		return err
	}
	for _, n := range networks {
		if ref := n.Labels[label]; ref != "" {
			if err := d.Attach(ctx, ref); err != nil {
				return err
			}
		}
	}
	return nil
}
