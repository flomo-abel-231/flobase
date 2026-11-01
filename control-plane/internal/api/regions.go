package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"

	"flobase/control-plane/internal/catalog"
	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

// Region servers: the owner adds a Docker host in some location (over SSH
// or TLS), FloBase starts its relay there, and once opened the region is
// offered for new projects. See provisioner/remote.go and relay.go.

func (s *Server) regionRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/regions", s.platformAdmin(s.listRegions))
	mux.Handle("POST /api/v1/regions", s.owner(s.addRegion))
	mux.Handle("PATCH /api/v1/regions/{id}", s.owner(s.updateRegion))
	mux.Handle("DELETE /api/v1/regions/{id}", s.owner(s.deleteRegion))
	mux.Handle("POST /api/v1/regions/{id}/check", s.owner(s.checkRegion))
}

func (s *Server) owner(next http.HandlerFunc) http.Handler {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		if who(r).role() != "owner" {
			writeError(w, http.StatusForbidden, "Only the owner can manage regions.")
			return
		}
		next(w, r)
	})
}

// driver is the provisioner driver of a region, or an error when the
// region has no server connected.
func (s *Server) driver(region string) (*provisioner.Docker, error) {
	if s.router == nil {
		return nil, nil
	}
	return s.router.Region(region)
}

func remoteOf(v store.RegionServer) provisioner.Remote {
	return provisioner.Remote{
		Transport: v.Transport, Endpoint: v.Endpoint,
		SSHKey: v.SSHKey, SSHHostKey: v.SSHHostKey,
		TLSCA: v.TLSCA, TLSCert: v.TLSCert, TLSKey: v.TLSKey,
		PublicHost: v.PublicHost,
		Relay: provisioner.RelayConfig{
			Port: v.RelayPort, ServerCert: v.RelayServerCert, ServerKey: v.RelayServerKey,
			ClientCert: v.RelayClientCert, ClientKey: v.RelayClientKey,
		},
	}
}

// StartRegions connects the drivers of every region server, then makes
// sure each relay runs. Call it before anything provisions.
func (s *Server) StartRegions(ctx context.Context) error {
	if s.router == nil {
		return nil
	}
	servers, err := s.store.RegionServers(ctx)
	if err != nil {
		return err
	}
	for _, v := range servers {
		d, err := provisioner.NewRemoteDocker(remoteOf(v), nil)
		if err != nil {
			s.log.Error("region server", "region", v.Region.ID, "err", err)
			continue
		}
		s.router.SetServer(v.Region.ID, d)
		go func(v store.RegionServer) {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			if err := d.EnsureRelay(ctx, remoteOf(v).Relay); err != nil {
				s.log.Warn("region server relay", "region", v.Region.ID, "err", err)
				return
			}
			s.log.Info("region server connected", "region", v.Region.ID, "endpoint", v.Endpoint)
		}(v)
	}
	return nil
}

type serverView struct {
	Transport  string    `json:"transport"`
	Endpoint   string    `json:"endpoint"`
	PublicHost string    `json:"public_host"`
	RelayPort  int       `json:"relay_port"`
	HostKey    string    `json:"ssh_host_key_fingerprint,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type regionView struct {
	catalog.Region
	// Server is nil for the local region.
	Server   *serverView `json:"server,omitempty"`
	Projects int         `json:"projects"`
}

func fingerprint(hostKey string) string {
	key, err := provisioner.ParseHostKey(hostKey)
	if err != nil {
		return ""
	}
	return ssh.FingerprintSHA256(key)
}

func (s *Server) listRegions(w http.ResponseWriter, r *http.Request) {
	regions, err := s.store.Regions(r.Context(), s.providers())
	if err != nil {
		s.internal(w, err)
		return
	}
	servers, err := s.store.RegionServers(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	counts, err := s.store.RegionProjects(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	byID := map[string]store.RegionServer{}
	for _, v := range servers {
		byID[v.Region.ID] = v
	}
	out := make([]regionView, 0, len(regions))
	for _, reg := range regions {
		v := regionView{Region: reg, Projects: counts[reg.ID]}
		if srv, ok := byID[reg.ID]; ok {
			v.Server = &serverView{
				Transport: srv.Transport, Endpoint: srv.Endpoint, PublicHost: srv.PublicHost,
				RelayPort: srv.RelayPort, HostKey: fingerprint(srv.SSHHostKey), CreatedAt: srv.CreatedAt,
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

type regionRequest struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Location        string  `json:"location"`
	PriceMultiplier float64 `json:"price_multiplier"`
	Transport       string  `json:"transport"`
	Endpoint        string  `json:"endpoint"`
	SSHKey          string  `json:"ssh_key"`
	SSHHostKey      string  `json:"ssh_host_key"`
	TLSCA           string  `json:"tls_ca"`
	TLSCert         string  `json:"tls_cert"`
	TLSKey          string  `json:"tls_key"`
	PublicHost      string  `json:"public_host"`
	RelayPort       int     `json:"relay_port"`
}

var regionID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// validHost accepts a DNS name or an IP address.
var hostName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,62}\.)*[A-Za-z0-9-]{1,63}$`)

func (req *regionRequest) check() string {
	req.ID = strings.ToLower(strings.TrimSpace(req.ID))
	req.Name, req.Location = strings.TrimSpace(req.Name), strings.TrimSpace(req.Location)
	req.Endpoint, req.PublicHost = strings.TrimSpace(req.Endpoint), strings.TrimSpace(req.PublicHost)
	switch {
	case !regionID.MatchString(req.ID) || req.ID == provisioner.LocalRegion:
		return "The region id is 2 to 32 lowercase letters, digits and dashes, such as eu-fra-1."
	case utf8.RuneCountInString(req.Name) < 1 || utf8.RuneCountInString(req.Name) > 64:
		return "Name must be 1 to 64 characters."
	case utf8.RuneCountInString(req.Location) > 64:
		return "The location label is at most 64 characters."
	case req.PriceMultiplier != 0 && (req.PriceMultiplier < 0.1 || req.PriceMultiplier > 10):
		return "The cost multiplier must be between 0.1 and 10."
	case req.RelayPort != 0 && (req.RelayPort < 1024 || req.RelayPort > 65535):
		return "The relay port must be between 1024 and 65535."
	case net.ParseIP(req.PublicHost) == nil && !hostName.MatchString(req.PublicHost):
		return "The public host is the name or IP address customers connect to, such as fra1.db.example.com."
	}
	if req.PriceMultiplier == 0 {
		req.PriceMultiplier = 1
	}
	if req.RelayPort == 0 {
		req.RelayPort = provisioner.DefaultRelayPort
	}
	u, err := url.Parse(req.Endpoint)
	switch req.Transport {
	case "ssh":
		if err != nil || u.Scheme != "ssh" || u.Hostname() == "" {
			return "An SSH endpoint looks like ssh://root@203.0.113.10 or ssh://deploy@fra1.example.com:2222."
		}
		if strings.TrimSpace(req.SSHKey) == "" {
			return "Paste the private key FloBase signs in with."
		}
	case "tls":
		if err != nil || u.Scheme != "tcp" || u.Hostname() == "" {
			return "A TLS endpoint looks like tcp://203.0.113.10:2376."
		}
		if strings.TrimSpace(req.TLSCA) == "" || strings.TrimSpace(req.TLSCert) == "" || strings.TrimSpace(req.TLSKey) == "" {
			return "Paste the CA certificate, client certificate and client key (ca.pem, cert.pem, key.pem)."
		}
	default:
		return "Choose SSH or TLS."
	}
	return ""
}

// connect makes a driver for a new or changed server and checks it end to
// end: the Docker API answers, the relay runs, and the relay accepts the
// control plane's certificate.
func (s *Server) connect(ctx context.Context, rm provisioner.Remote) (*provisioner.Docker, error) {
	d, err := provisioner.NewRemoteDocker(rm, nil)
	if err != nil {
		return nil, err
	}
	if err := d.Ping(ctx); err != nil {
		return nil, errors.New("cannot reach Docker: " + err.Error())
	}
	if err := d.EnsureRelay(ctx, rm.Relay); err != nil {
		return nil, errors.New("cannot start the relay: " + err.Error())
	}
	relay, err := provisioner.NewRelay(rm.Relay, rm.PublicHost)
	if err != nil {
		return nil, err
	}
	// The relay may need a moment after its container starts.
	for attempt := 0; ; attempt++ {
		if err = relay.Check(ctx); err == nil || attempt == 10 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, errors.New("Docker answers, but the relay is not reachable at " + rm.PublicHost + ": " + err.Error() +
			". Open the relay port to the control plane in the server's firewall.")
	}
	return d, nil
}

func (s *Server) addRegion(w http.ResponseWriter, r *http.Request) {
	if s.router == nil {
		writeError(w, http.StatusNotImplemented, "Region servers are not available here.")
		return
	}
	var req regionRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if _, err := s.store.Region(r.Context(), req.ID); err == nil {
		writeError(w, http.StatusConflict, "A region with id "+req.ID+" exists.")
		return
	}
	relay, err := provisioner.NewRelayCerts(req.RelayPort)
	if err != nil {
		s.internal(w, err)
		return
	}
	v := store.RegionServer{
		// New regions start closed: the owner opens them once ready.
		Region: catalog.Region{
			ID: req.ID, Name: req.Name, Provider: "docker", Location: req.Location, Status: "closed",
			PriceMultiplier: req.PriceMultiplier,
		},
		Transport: req.Transport, Endpoint: req.Endpoint, PublicHost: req.PublicHost, RelayPort: req.RelayPort,
		RelayServerCert: relay.ServerCert, RelayServerKey: relay.ServerKey,
		RelayClientCert: relay.ClientCert, RelayClientKey: relay.ClientKey,
	}
	if req.Transport == "ssh" {
		v.SSHKey = strings.TrimSpace(req.SSHKey) + "\n"
		if strings.TrimSpace(req.SSHHostKey) != "" {
			key, err := provisioner.ParseHostKey(req.SSHHostKey)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			v.SSHHostKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
		}
	} else {
		v.TLSCA, v.TLSCert, v.TLSKey = req.TLSCA, req.TLSCert, req.TLSKey
	}
	// Pulling the relay image on a fresh server can take a while.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	d, err := s.connect(ctx, remoteOf(v))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if v.Transport == "ssh" && v.SSHHostKey == "" {
		// Trust on first use: this key is required from now on.
		v.SSHHostKey = d.HostKey()
	}
	if err := s.store.CreateRegionServer(r.Context(), v); errors.Is(err, store.ErrRegionExists) {
		writeError(w, http.StatusConflict, "A region with id "+req.ID+" exists.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	// A fresh driver pinned to the recorded host key.
	if d, err = provisioner.NewRemoteDocker(remoteOf(v), nil); err != nil {
		s.internal(w, err)
		return
	}
	s.router.SetServer(v.Region.ID, d)
	s.log.Info("region server added", "region", v.Region.ID, "endpoint", v.Endpoint)
	writeJSON(w, http.StatusCreated, regionView{Region: v.Region, Server: &serverView{
		Transport: v.Transport, Endpoint: v.Endpoint, PublicHost: v.PublicHost, RelayPort: v.RelayPort,
		HostKey: fingerprint(v.SSHHostKey), CreatedAt: time.Now(),
	}})
}

func (s *Server) updateRegion(w http.ResponseWriter, r *http.Request) {
	reg, err := s.store.Region(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Region not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	var req struct {
		Name            *string  `json:"name"`
		Location        *string  `json:"location"`
		Status          *string  `json:"status"`
		PriceMultiplier *float64 `json:"price_multiplier"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Name != nil {
		reg.Name = strings.TrimSpace(*req.Name)
		if n := utf8.RuneCountInString(reg.Name); n < 1 || n > 64 {
			writeError(w, http.StatusBadRequest, "Name must be 1 to 64 characters.")
			return
		}
	}
	if req.Location != nil {
		reg.Location = strings.TrimSpace(*req.Location)
		if utf8.RuneCountInString(reg.Location) > 64 {
			writeError(w, http.StatusBadRequest, "The location label is at most 64 characters.")
			return
		}
	}
	if req.PriceMultiplier != nil {
		if *req.PriceMultiplier < 0.1 || *req.PriceMultiplier > 10 {
			writeError(w, http.StatusBadRequest, "The cost multiplier must be between 0.1 and 10.")
			return
		}
		reg.PriceMultiplier = *req.PriceMultiplier
	}
	if req.Status != nil {
		if !contains([]string{"open", "full", "closed"}, *req.Status) {
			writeError(w, http.StatusBadRequest, "Status must be open, full or closed.")
			return
		}
		if *req.Status == "open" {
			if _, err := s.driver(reg.ID); err != nil {
				writeError(w, http.StatusConflict, "The region's server is not connected.")
				return
			}
		}
		reg.Status = *req.Status
	}
	if err := s.store.UpdateRegion(r.Context(), reg); err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reg)
}

func (s *Server) deleteRegion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.store.DeleteRegionServer(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Region server not found.")
		return
	case errors.Is(err, store.ErrRegionInUse):
		writeError(w, http.StatusConflict, "Delete or move the region's projects first.")
		return
	case err != nil:
		s.internal(w, err)
		return
	}
	// The relay keeps running on the server until removed there; it only
	// lets in the certificate this control plane just forgot.
	s.router.SetServer(id, nil)
	s.log.Info("region server removed", "region", id)
	w.WriteHeader(http.StatusNoContent)
}

// checkRegion reconnects to a region server, restarting its relay if it
// stopped.
func (s *Server) checkRegion(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.RegionServer(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Region server not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	d, err := s.connect(ctx, remoteOf(v))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.router.SetServer(v.Region.ID, d)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
