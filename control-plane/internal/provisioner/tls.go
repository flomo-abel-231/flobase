package provisioner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// Database connections are encrypted with TLS. Each FloBase install has its
// own certificate authority, kept in the platform database, which issues
// every project a server certificate for the host clients connect to.
// Clients encrypt with sslmode=require (or the engine's equivalent) and can
// verify the server against the CA, which the dashboard offers for download.

// CA is the install's certificate authority.
type CA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	CertPEM string
}

// NewCAPEM makes a certificate authority and returns it as PEM.
func NewCAPEM() (certPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return "", "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "FloBase database CA", Organization: []string{"FloBase"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
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

// LoadCA reads a certificate authority made by NewCAPEM.
func LoadCA(certPEM, keyPEM string) (*CA, error) {
	cb, _ := pem.Decode([]byte(certPEM))
	kb, _ := pem.Decode([]byte(keyPEM))
	if cb == nil || kb == nil {
		return nil, errors.New("the database CA is not valid PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, err
	}
	return &CA{cert: cert, key: key, CertPEM: certPEM}, nil
}

// Issue makes a server certificate for the given host names and addresses.
func (ca *CA) Issue(hosts []string) (certPEM, keyPEM string, err error) {
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
		Subject:      pkix.Name{CommonName: hosts[0], Organization: []string{"FloBase"}},
		NotBefore:    time.Now().Add(-time.Hour),
		// Long-lived: projects are given a fresh one whenever their
		// containers are recreated.
		NotAfter:    time.Now().AddDate(10, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
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

// SetCA turns on TLS for projects this driver creates or recreates.
func (d *Docker) SetCA(ca *CA) {
	d.offsiteMu.Lock()
	d.ca = ca
	d.offsiteMu.Unlock()
}

// withTLS gives s a server certificate for this host, when TLS is on.
func (d *Docker) withTLS(s Spec) (Spec, error) {
	d.offsiteMu.RLock()
	ca := d.ca
	d.offsiteMu.RUnlock()
	if ca == nil || s.Engine == "mysql" {
		// MySQL makes and uses its own certificate on first start.
		return s, nil
	}
	hosts := []string{d.publicHost}
	for _, h := range []string{"localhost", "127.0.0.1", "flobase-" + s.Ref + "-db", "flobase-" + s.Ref + "-pooler"} {
		if h != d.publicHost {
			hosts = append(hosts, h)
		}
	}
	var err error
	s.TLSCert, s.TLSKey, err = ca.Issue(hosts)
	return s, err
}

// tlsDir is where project containers find their certificate.
const tlsDir = "/tls"

// tlsMounts mounts the project's certificate volume read-only, when TLS is
// on.
func tlsMounts(s Spec) []mount {
	if s.TLSCert == "" {
		return nil
	}
	return []mount{{volume: vol(s.Ref, "tls"), path: tlsDir, readOnly: true}}
}

// tlsScript writes the certificate into the tls volume, readable by the
// pooler's and FerretDB's own users; only this project's containers mount
// it. With CONFIG set it also gives Postgres its own copy, owned by it as
// Postgres requires, and turns TLS on.
const tlsScript = `set -e
mkdir -p /tls-out
printf '%s' "$TLS_CERT" > /tls-out/server.crt
printf '%s' "$TLS_KEY" > /tls-out/server.key
chmod 644 /tls-out/server.crt /tls-out/server.key
if [ -n "$CONFIG" ]; then
  mkdir -p "$CONFIG/tls"
  cp /tls-out/server.crt /tls-out/server.key "$CONFIG/tls/"
  chown -R postgres:postgres "$CONFIG/tls"
  chmod 600 "$CONFIG/tls/server.key"
  cat > "$CONFIG/conf.d/flobase-tls.conf" <<CONF
ssl = on
ssl_cert_file = '$CONFIG/tls/server.crt'
ssl_key_file = '$CONFIG/tls/server.key'
ssl_min_protocol_version = 'TLSv1.2'
CONF
  chown postgres:postgres "$CONFIG/conf.d/flobase-tls.conf"
fi`

// tlsContainer writes a project's certificate before its services start,
// and again whenever they are recreated, so certificates renew.
func tlsContainer(s Spec, image string, extra ...mount) container {
	env := []string{"TLS_CERT=" + s.TLSCert, "TLS_KEY=" + s.TLSKey}
	if len(extra) > 0 {
		env = append(env, "CONFIG="+extra[0].path)
	}
	return container{
		suffix: "tls", image: image, user: "root", oneShot: true, everyStart: true,
		entrypoint: []string{"sh", "-c"}, cmd: []string{tlsScript}, env: env,
		mounts: append([]mount{{volume: vol(s.Ref, "tls"), path: "/tls-out"}}, extra...),
	}
}

// poolerTLSEnv makes PgBouncer accept encrypted connections only.
func poolerTLSEnv(s Spec) []string {
	if s.TLSCert == "" {
		return nil
	}
	return []string{
		"CLIENT_TLS_SSLMODE=require", "CLIENT_TLS_CERT_FILE=" + tlsDir + "/server.crt",
		"CLIENT_TLS_KEY_FILE=" + tlsDir + "/server.key",
	}
}

// mongoTLS writes the certificate for a MongoDB-compatible project; the
// FerretDB image has no shell, so the Postgres image next to it does it.
func mongoTLS(s Spec) []container {
	if s.TLSCert == "" {
		return nil
	}
	return []container{tlsContainer(s, "ghcr.io/ferretdb/postgres-documentdb:17")}
}

// ferretTLSEnv makes FerretDB serve MongoDB clients over TLS only: the
// published port speaks TLS, and the plain listener stays inside the
// container.
func ferretTLSEnv(s Spec) []string {
	if s.TLSCert == "" {
		return nil
	}
	return []string{
		"FERRETDB_LISTEN_TLS=:27017", "FERRETDB_LISTEN_ADDR=127.0.0.1:27019",
		"FERRETDB_LISTEN_TLS_CERT_FILE=" + tlsDir + "/server.crt",
		"FERRETDB_LISTEN_TLS_KEY_FILE=" + tlsDir + "/server.key",
	}
}

// runOnce runs a one-shot container to completion.
func (d *Docker) runOnce(ctx context.Context, s Spec, c container, network string) error {
	if err := d.pull(ctx, c.image); err != nil {
		return fmt.Errorf("pull %s: %w", c.image, err)
	}
	name := "flobase-" + s.Ref + "-" + c.suffix
	_ = d.call(ctx, "DELETE", "/containers/"+name+"?force=true", nil, nil)
	if err := d.run(ctx, s, c, name, network); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	_, err := d.waitExit(ctx, name)
	return err
}

// EnsureTLS turns TLS on for a project created before FloBase encrypted
// database connections: it writes the certificate, has Postgres reload its
// settings without a restart, and replaces the pooler (or FerretDB) on the
// same port. A project that has TLS already is left alone.
func (d *Docker) EnsureTLS(ctx context.Context, s Spec) (bool, error) {
	front := map[string]string{"postgres": "pooler", "mongodb": "db"}[s.Engine]
	if front == "" {
		return false, nil
	}
	s, err := d.withTLS(s)
	if err != nil || s.TLSCert == "" {
		return false, err
	}
	name := "flobase-" + s.Ref + "-" + front
	var current struct{ Config struct{ Env []string } }
	if err := d.call(ctx, "GET", "/containers/"+name+"/json", nil, &current); err != nil {
		return false, err
	}
	for _, e := range current.Config.Env {
		if strings.HasPrefix(e, "CLIENT_TLS_SSLMODE=") || strings.HasPrefix(e, "FERRETDB_LISTEN_TLS=") {
			return false, nil
		}
	}
	specs, _, err := containers(s)
	if err != nil {
		return false, err
	}
	network := "flobase-" + s.Ref
	for _, c := range specs {
		if c.everyStart {
			if err := d.runOnce(ctx, s, c, network); err != nil {
				return false, err
			}
		}
	}
	if s.Engine == "postgres" {
		if _, err := d.exec(ctx, "flobase-"+s.Ref+"-db", "postgres", "pg_ctl", "reload", "-D", "/var/lib/postgresql/data"); err != nil {
			return false, fmt.Errorf("reload Postgres settings: %w", err)
		}
	}
	for _, c := range specs {
		if c.suffix != front {
			continue
		}
		if c.hostPort, err = d.hostPort(ctx, name, c.port); err != nil {
			return false, err
		}
		if err := d.call(ctx, "DELETE", "/containers/"+name+"?force=true", nil, nil); err != nil && !isNotFound(err) {
			return false, fmt.Errorf("remove %s: %w", name, err)
		}
		if err := d.run(ctx, s, c, name, network); err != nil {
			return false, fmt.Errorf("start %s: %w", name, err)
		}
	}
	return true, nil
}
