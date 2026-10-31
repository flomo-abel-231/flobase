// Command flobase-gateway is the API gateway in front of every project's
// services. It routes /project/<ref>/<service>/v1/... to that project's
// containers, checks the API key and adds CORS, the job Kong or Envoy does
// in Supabase. It reads project records but never writes them, and the
// control plane is never on this request path.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"flobase/control-plane/internal/store"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Connect(ctx, env("FLOBASE_DATABASE_URL", "postgres://flobase:flobase@localhost:5433/flobase"))
	if err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	gw := &gateway{store: st, log: log, cache: map[string]cached{}}
	addr := env("FLOBASE_GATEWAY_ADDR", ":8000")
	srv := &http.Server{Addr: addr, Handler: gw, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("FloBase gateway listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

type cached struct {
	project store.Project
	found   bool
	at      time.Time
}

type gateway struct {
	store *store.Store
	log   *slog.Logger
	mu    sync.Mutex
	cache map[string]cached
}

// Project records change rarely; a short cache keeps the platform database
// off the hot path while picking up new keys and deletions within seconds.
const cacheFor = 10 * time.Second

func (g *gateway) project(ctx context.Context, ref string) (store.Project, bool, error) {
	g.mu.Lock()
	c, ok := g.cache[ref]
	g.mu.Unlock()
	if ok && time.Since(c.at) < cacheFor {
		return c.project, c.found, nil
	}
	p, err := g.store.Project(ctx, ref)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return p, false, err
	}
	c = cached{project: p, found: err == nil, at: time.Now()}
	g.mu.Lock()
	g.cache[ref] = c
	g.mu.Unlock()
	return c.project, c.found, nil
}

// Auth routes a browser opens from an email or OAuth redirect carry no key.
var openAuthPaths = []string{"/verify", "/callback", "/authorize", "/health", "/settings"}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/health" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	// /project/<ref>/<service>/v1/<rest>
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/project/"), "/", 4)
	if !strings.HasPrefix(r.URL.Path, "/project/") || len(parts) < 3 || parts[2] != "v1" {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Use /project/<ref>/rest/v1, /auth/v1 or /graphql/v1."})
		return
	}
	ref, service, rest := parts[0], parts[1], "/"
	if len(parts) == 4 {
		rest += parts[3]
	}
	p, found, err := g.project(r.Context(), ref)
	if err != nil {
		g.log.Error("project lookup", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "Gateway cannot read projects right now."})
		return
	}
	if !found || p.Engine != "postgres" {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "No such project."})
		return
	}
	if p.Status != store.StatusActive {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "The project is not active."})
		return
	}

	key := r.Header.Get("apikey")
	if key == "" {
		key = r.URL.Query().Get("apikey")
	}
	open := service == "auth" && contains(openAuthPaths, rest)
	if !open && !validKey(key, p) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Invalid API key", "hint": "Send the project's anon or service_role key in the apikey header."})
		return
	}
	// Without a user session the key itself is the bearer token, so
	// PostgREST runs the request as anon or service_role.
	if r.Header.Get("Authorization") == "" && key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}

	var target string
	switch service {
	case "rest":
		target = "http://flobase-" + ref + "-rest:3000" + rest
	case "auth":
		target = "http://flobase-" + ref + "-auth:9999" + rest
	case "graphql":
		// pg_graphql is served through PostgREST's RPC, as in Supabase.
		target = "http://flobase-" + ref + "-rest:3000/rpc/graphql"
		r.Header.Set("Content-Profile", "graphql_public")
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Unknown service " + service + "."})
		return
	}
	u, _ := url.Parse(target)
	u.RawQuery = r.URL.RawQuery
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = u
			pr.Out.Host = u.Host
			pr.SetXForwarded()
		},
		ModifyResponse: func(resp *http.Response) error {
			// The gateway owns CORS; drop the services' own headers so
			// browsers do not see them twice.
			for k := range resp.Header {
				if strings.HasPrefix(strings.ToLower(k), "access-control-") {
					resp.Header.Del(k)
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			g.log.Warn("upstream", "ref", ref, "service", service, "err", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"message": "The " + service + " service did not answer."})
		},
	}
	proxy.ServeHTTP(w, r)
}

func validKey(key string, p store.Project) bool {
	return key != "" && (subtle.ConstantTimeCompare([]byte(key), []byte(p.AnonKey)) == 1 ||
		subtle.ConstantTimeCompare([]byte(key), []byte(p.ServiceKey)) == 1)
}

func cors(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
	h.Set("Access-Control-Allow-Headers", "authorization, apikey, content-type, x-client-info, prefer, range, accept-profile, content-profile, x-supabase-api-version, accept")
	h.Set("Access-Control-Expose-Headers", "content-range, content-location, x-total-count")
	h.Set("Access-Control-Max-Age", "3600")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
