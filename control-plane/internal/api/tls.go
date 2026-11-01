package api

import (
	"context"
	"net/http"
	"time"

	"flobase/control-plane/internal/catalog"
	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

// Database TLS: the install's certificate authority lives in the platform
// database and issues every project's server certificate (see
// provisioner/tls.go). Clients encrypt with the sslmode=require in their
// connection string and can verify the server with the CA from
// GET /api/v1/tls/ca.

func (s *Server) tlsRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/tls/ca", s.auth(s.caCert))
}

// loadCA reads the install's CA, making it on first start.
func (s *Server) loadCA(ctx context.Context) (*provisioner.CA, error) {
	certPEM, err := s.store.Setting(ctx, "tls.ca_cert", "")
	if err != nil {
		return nil, err
	}
	keyPEM, err := s.store.Setting(ctx, "tls.ca_key", "")
	if err != nil {
		return nil, err
	}
	if certPEM == "" || keyPEM == "" {
		if certPEM, keyPEM, err = provisioner.NewCAPEM(); err != nil {
			return nil, err
		}
		if err := s.store.SetSetting(ctx, "tls.ca_key", keyPEM); err != nil {
			return nil, err
		}
		if err := s.store.SetSetting(ctx, "tls.ca_cert", certPEM); err != nil {
			return nil, err
		}
	}
	return provisioner.LoadCA(certPEM, keyPEM)
}

// StartTLS turns on database TLS and, in the background, gives projects
// made before it their certificate. Call it after StartRegions.
func (s *Server) StartTLS(ctx context.Context) error {
	if s.router == nil {
		return nil
	}
	ca, err := s.loadCA(ctx)
	if err != nil {
		return err
	}
	s.ca = ca
	s.router.SetCA(ca)
	projects, err := s.store.Projects(ctx)
	if err != nil {
		return err
	}
	go func() {
		for _, p := range projects {
			if p.TLS || p.Status != store.StatusActive || (p.Engine != "postgres" && p.Engine != "mongodb") {
				continue
			}
			plan, _ := catalog.PlanByID(p.Plan)
			spec := provisioner.Spec{
				Ref: p.Ref, Region: p.Region, Engine: p.Engine, Plan: plan, User: p.DBUser, Password: p.DBPassword,
				Database: p.DBName, JWTSecret: p.JWTSecret, AuthURL: s.apiURL(p.Ref) + "/auth/v1",
			}
			pctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			changed, err := s.prov.EnsureTLS(pctx, spec)
			cancel()
			if err != nil {
				s.log.Warn("turn on database TLS", "ref", p.Ref, "err", err)
				continue
			}
			if err := s.store.SetTLS(ctx, p.Ref, true); err != nil {
				s.log.Warn("record database TLS", "ref", p.Ref, "err", err)
				continue
			}
			s.log.Info("database TLS on", "ref", p.Ref, "changed", changed)
		}
	}()
	return nil
}

func (s *Server) caCert(w http.ResponseWriter, r *http.Request) {
	if s.ca == nil {
		writeError(w, http.StatusNotFound, "Database TLS is not set up on this FloBase.")
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="flobase-ca.pem"`)
	_, _ = w.Write([]byte(s.ca.CertPEM))
}
