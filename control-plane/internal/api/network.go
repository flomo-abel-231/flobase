package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

// Network restrictions: each project may list the addresses allowed to
// reach its database and pooler ports. FloBase enforces them on those
// ports only (see provisioner/firewall.go); the rest of the server, the
// dashboard and the project's REST and auth APIs are not affected.

func (s *Server) networkRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/projects/{ref}/network", s.auth(s.getNetwork))
	mux.Handle("PUT /api/v1/projects/{ref}/network", s.auth(s.setNetwork))
}

const maxAllowed = 50

// normalizeCIDRs checks addresses and ranges, writing single addresses as
// /32 or /128. It returns a message for the first bad entry.
func normalizeCIDRs(in []string) ([]string, string) {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		var cidr string
		if ip := net.ParseIP(v); ip != nil {
			if ip.To4() != nil {
				cidr = ip.String() + "/32"
			} else {
				cidr = ip.String() + "/128"
			}
		} else if _, n, err := net.ParseCIDR(v); err == nil {
			cidr = n.String()
		} else {
			return nil, v + " is not an IP address or range such as 203.0.113.7 or 203.0.113.0/24."
		}
		if !seen[cidr] {
			seen[cidr] = true
			out = append(out, cidr)
		}
	}
	if len(out) > maxAllowed {
		return nil, "Allow at most 50 addresses or ranges; use a wider range instead."
	}
	return out, ""
}

// serverIPs are the public addresses of the host a region's projects run
// on, from its public host name. Allowing one of them also lets in apps on
// that host, which is what people mean by allowing "the server".
func (s *Server) serverIPs(ctx context.Context, region string) []net.IP {
	if s.router == nil {
		return nil
	}
	d, err := s.router.Region(region)
	if err != nil {
		return nil
	}
	host := d.PublicHost()
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return nil
		}
		return []net.IP{ip}
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, a := range addrs {
		if !a.IP.IsLoopback() {
			out = append(out, a.IP)
		}
	}
	return out
}

// coversServer is true when one of the allowed ranges contains one of the
// server's addresses.
func coversServer(allowed []string, ips []net.IP) bool {
	for _, c := range allowed {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		for _, ip := range ips {
			if n.Contains(ip) {
				return true
			}
		}
	}
	return false
}

func ipStrings(ips []net.IP) []string {
	out := []string{}
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

func (s *Server) networkView(r *http.Request, p store.Project, allowed []string) map[string]any {
	if allowed == nil {
		allowed = []string{}
	}
	ips := s.serverIPs(r.Context(), p.Region)
	return map[string]any{
		"allowed": allowed, "your_ip": clientIP(r),
		"server_ips": ipStrings(ips), "server_allowed": coversServer(allowed, ips),
	}
}

func (s *Server) getNetwork(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Project(r.Context(), r.PathValue("ref"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Project not found.")
		return
	}
	writeJSON(w, http.StatusOK, s.networkView(r, p, p.AllowedCIDRs))
}

func (s *Server) setNetwork(w http.ResponseWriter, r *http.Request) {
	if !who(r).canManageTeam() {
		writeError(w, http.StatusForbidden, "Only the workspace's owner and admins can change network restrictions.")
		return
	}
	p, ok := s.activeProject(w, r)
	if !ok {
		return
	}
	var req struct {
		Allowed []string `json:"allowed"`
	}
	if !decode(w, r, &req) {
		return
	}
	allowed, msg := normalizeCIDRs(req.Allowed)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.SetAllowedCIDRs(r.Context(), p.Ref, allowed); err != nil {
		s.internal(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
	defer cancel()
	if err := s.applyFirewall(ctx, p.Region); err != nil {
		s.log.Error("apply network restrictions", "ref", p.Ref, "err", err)
		writeError(w, http.StatusBadGateway, "Saved, but the server's firewall could not be updated: "+err.Error())
		return
	}
	s.log.Info("network restrictions set", "ref", p.Ref, "allowed", len(allowed))
	writeJSON(w, http.StatusOK, s.networkView(r, p, allowed))
}

// applyFirewall gives a region's host the restrictions of all its
// projects.
func (s *Server) applyFirewall(ctx context.Context, region string) error {
	if s.router == nil {
		return nil
	}
	projects, err := s.store.Projects(ctx)
	if err != nil {
		return err
	}
	ips := s.serverIPs(ctx, region)
	var rules []provisioner.PortRule
	for _, p := range projects {
		if p.Region != region || len(p.AllowedCIDRs) == 0 || p.Status == store.StatusDeleting {
			continue
		}
		local := coversServer(p.AllowedCIDRs, ips)
		for _, port := range []int{p.DBPort, p.PoolerPort} {
			if port > 0 {
				rules = append(rules, provisioner.PortRule{Port: port, Allow: p.AllowedCIDRs, Local: local})
			}
		}
	}
	return s.router.ApplyFirewall(ctx, region, rules)
}

// StartFirewall reapplies every region's restrictions, in the background.
func (s *Server) StartFirewall(ctx context.Context) {
	if s.router == nil {
		return
	}
	regions, err := s.store.Regions(ctx, s.providers())
	if err != nil {
		s.log.Warn("network restrictions", "err", err)
		return
	}
	go func() {
		for _, reg := range regions {
			rctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			if err := s.applyFirewall(rctx, reg.ID); err != nil {
				s.log.Warn("apply network restrictions", "region", reg.ID, "err", err)
			}
			cancel()
		}
	}()
}
