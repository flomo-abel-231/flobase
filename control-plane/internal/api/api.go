// Package api is the control plane's HTTP API, used by the dashboard and CLI.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"flobase/control-plane/internal/catalog"
	"flobase/control-plane/internal/keys"
	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

type Config struct {
	Mode       string // "self-hosted" or "cloud"
	Version    string
	APIToken   string
	Markup     float64
	GatewayURL string // public base URL of the API gateway, e.g. http://localhost:28471
	// EnvOffsite is off-site storage set in .env; it takes precedence over
	// locations added in the dashboard (see storage.go).
	EnvOffsite *provisioner.Offsite
	// Self-update: the GitHub repository releases come from, whether to
	// install them without asking, and where the install script put FloBase
	// on the host (empty = not installed by it, so no self-update).
	UpdateRepo string
	AutoUpdate bool
	InstallDir string
}

type Server struct {
	cfg   Config
	store *store.Store
	prov  provisioner.Provisioner
	log   *slog.Logger
	jobs  backupJobs
	// updater is nil when the provisioner cannot start the updater.
	updater Updater
	updates updateState
}

func New(cfg Config, st *store.Store, prov provisioner.Provisioner, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, store: st, prov: prov, log: log, jobs: backupJobs{running: map[string]bool{}, sources: map[string]int{}}}
	if u, ok := prov.(Updater); ok {
		s.updater = u
	}
	return s
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/config", s.config)
	mux.Handle("GET /api/v1/me", s.auth(s.me))
	mux.Handle("GET /api/v1/catalog", s.auth(s.catalog))
	mux.Handle("GET /api/v1/projects", s.auth(s.listProjects))
	mux.Handle("POST /api/v1/projects", s.auth(s.createProject))
	mux.Handle("GET /api/v1/projects/{ref}", s.auth(s.getProject))
	mux.Handle("DELETE /api/v1/projects/{ref}", s.auth(s.deleteProject))
	s.dataRoutes(mux)
	s.backupRoutes(mux)
	s.storageRoutes(mux)
	s.deletionRoutes(mux)
	s.updateRoutes(mux)
	return mux
}

func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.APIToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "Missing or wrong API token.")
			return
		}
		next(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"mode": s.cfg.Mode, "version": s.cfg.Version, "billing": s.billing()})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"role": "admin"})
}

func (s *Server) billing() bool { return s.cfg.Mode == "cloud" }

// providers lists which regions this mode can show.
func (s *Server) providers() []string {
	if s.cfg.Mode == "cloud" {
		return []string{"docker", "aws", "gcp", "azure"}
	}
	return []string{"docker"}
}

type planView struct {
	catalog.Plan
	Markup float64 `json:"markup,omitempty"`
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	regions, err := s.store.Regions(r.Context(), s.providers())
	if err != nil {
		s.internal(w, err)
		return
	}
	plans := make([]planView, 0, len(catalog.Plans))
	for _, p := range catalog.Plans {
		v := planView{Plan: p}
		if s.billing() {
			if p.EstCost > 0 && p.Price > 0 {
				v.Markup = round2(p.Price / p.EstCost)
			}
		} else {
			// Self-hosted has no billing: sizes only.
			v.EstCost, v.Price = 0, 0
		}
		plans = append(plans, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"engines": catalog.Engines, "plans": plans, "regions": regions,
		"billing": s.billing(), "target_markup": s.cfg.Markup,
	})
}

type connection struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	Password  string `json:"password"`
	Database  string `json:"database"`
	URI       string `json:"uri"`
	PoolerURI string `json:"pooler_uri,omitempty"`
}

type projectView struct {
	store.Project
	Connection *connection       `json:"connection,omitempty"`
	Keys       map[string]string `json:"keys,omitempty"`
	APIURL     string            `json:"api_url,omitempty"`
}

func (s *Server) view(p store.Project, withSecrets bool) projectView {
	v := projectView{Project: p}
	if !withSecrets {
		return v
	}
	v.Keys = map[string]string{"anon": p.AnonKey, "service_role": p.ServiceKey}
	if p.Engine == "postgres" {
		v.APIURL = s.apiURL(p.Ref)
	}
	if p.Status == store.StatusActive {
		v.Connection = &connection{
			Host: p.DBHost, Port: p.DBPort, User: p.DBUser, Password: p.DBPassword, Database: p.DBName,
			URI: connectionURI(p, p.DBPort),
		}
		if p.PoolerPort > 0 {
			v.Connection.PoolerURI = connectionURI(p, p.PoolerPort)
		}
	}
	return v
}

// apiURL is the base URL client SDKs take: supabase-js appends /rest/v1,
// /auth/v1 and /graphql/v1 to it.
func (s *Server) apiURL(ref string) string {
	return strings.TrimRight(s.cfg.GatewayURL, "/") + "/project/" + ref
}

func connectionURI(p store.Project, port int) string {
	scheme := map[string]string{"postgres": "postgresql", "mysql": "mysql", "mongodb": "mongodb"}[p.Engine]
	u := url.URL{
		Scheme: scheme,
		User:   url.UserPassword(p.DBUser, p.DBPassword),
		Host:   p.DBHost + ":" + strconv.Itoa(port),
		Path:   "/" + p.DBName,
	}
	return u.String()
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.Projects(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	out := make([]projectView, 0, len(projects))
	for _, p := range projects {
		out = append(out, s.view(p, false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Project(r.Context(), r.PathValue("ref"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Project not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.view(p, true))
}

type createRequest struct {
	Name   string `json:"name"`
	Engine string `json:"engine"`
	Region string `json:"region"`
	Plan   string `json:"plan"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Body must be JSON with name, engine, region and plan.")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if n := utf8.RuneCountInString(req.Name); n < 1 || n > 64 {
		writeError(w, http.StatusBadRequest, "Name must be 1 to 64 characters.")
		return
	}
	if _, ok := catalog.EngineByID(req.Engine); !ok {
		writeError(w, http.StatusBadRequest, "Unknown engine.")
		return
	}
	plan, ok := catalog.PlanByID(req.Plan)
	if !ok {
		writeError(w, http.StatusBadRequest, "Unknown plan.")
		return
	}
	region, err := s.store.Region(r.Context(), req.Region)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !contains(s.providers(), region.Provider)) {
		writeError(w, http.StatusBadRequest, "Unknown region.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	if region.Status != "open" || region.Provider != "docker" {
		writeError(w, http.StatusConflict, region.Name+" is not open for new projects yet.")
		return
	}

	ref := keys.Ref()
	secret := keys.JWTSecret()
	// Postgres projects follow Supabase's conventions so its SDKs and tools
	// work unchanged.
	user, dbname := "flobase", "app"
	if req.Engine == "postgres" {
		user, dbname = "postgres", "postgres"
	}
	p, err := s.store.CreateProject(r.Context(), store.Project{
		Ref: ref, Name: req.Name, Engine: req.Engine, Region: region.ID, Plan: plan.ID,
		DBUser: user, DBPassword: keys.Password(), DBName: dbname,
		JWTSecret: secret,
		AnonKey:   keys.APIKey(secret, ref, "anon"), ServiceKey: keys.APIKey(secret, ref, "service_role"),
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	go s.provision(p, plan, nil)
	writeJSON(w, http.StatusAccepted, s.view(p, true))
}

// provision runs outside the request: the dashboard polls the project's status.
func (s *Server) provision(p store.Project, plan catalog.Plan, restore *provisioner.Restore) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	start := time.Now()
	ep, err := s.prov.Create(ctx, provisioner.Spec{
		Ref: p.Ref, Engine: p.Engine, Plan: plan, User: p.DBUser, Password: p.DBPassword, Database: p.DBName,
		JWTSecret: p.JWTSecret, AuthURL: s.apiURL(p.Ref) + "/auth/v1", Restore: restore,
	})
	if err != nil {
		s.log.Error("provision failed", "ref", p.Ref, "err", err)
		_ = s.store.SetStatus(context.Background(), p.Ref, store.StatusFailed, err.Error())
		return
	}
	if err := s.store.SetActive(context.Background(), p.Ref, ep.Host, ep.Port, ep.PoolerPort); err != nil {
		s.log.Error("save endpoint", "ref", p.Ref, "err", err)
		return
	}
	s.log.Info("project active", "ref", p.Ref, "engine", p.Engine, "port", ep.Port, "took", time.Since(start).Round(time.Millisecond))
	// The first backup starts the restore window right away; the off-site
	// sync then copies it and every change after.
	if p.Engine == "postgres" {
		s.ensureOffsite(ctx, p)
		go s.backup(p.Ref, "first")
	}
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Project(r.Context(), r.PathValue("ref"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Project not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	if p.Status == store.StatusProvisioning {
		writeError(w, http.StatusConflict, "Wait for provisioning to finish before deleting.")
		return
	}
	if s.jobs.restoring(p.Ref) {
		writeError(w, http.StatusConflict, "A restore from this project is running. Delete it once the restore finishes.")
		return
	}
	// The project's name, typed by the person deleting it, guards against
	// deleting the wrong project.
	var req struct {
		Confirm string `json:"confirm"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req)
	if strings.TrimSpace(req.Confirm) != p.Name {
		writeError(w, http.StatusBadRequest, "Type the project's name to confirm deleting it.")
		return
	}
	if err := s.store.SetStatus(r.Context(), p.Ref, store.StatusDeleting, ""); err != nil {
		s.internal(w, err)
		return
	}
	go s.Teardown(p.Ref)
	w.WriteHeader(http.StatusAccepted)
}

// Teardown removes a project's infrastructure, then its record.
func (s *Server) Teardown(ref string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := s.prov.Delete(ctx, ref); err != nil {
		s.log.Error("delete failed", "ref", ref, "err", err)
		_ = s.store.SetStatus(context.Background(), ref, store.StatusFailed, "Delete failed: "+err.Error())
		return
	}
	if err := s.store.DeleteProject(context.Background(), ref); err != nil {
		s.log.Error("delete record", "ref", ref, "err", err)
		return
	}
	s.log.Info("project deleted", "ref", ref)
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	s.log.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "Something went wrong. Check the control-plane logs.")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
