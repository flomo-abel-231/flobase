// Command flobase-api runs the FloBase control plane: the HTTP API, the
// provisioner and, when FLOBASE_DASHBOARD_DIR is set, the built dashboard.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"flobase/control-plane/internal/api"
	"flobase/control-plane/internal/catalog"
	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

var version = "dev"

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	mode := env("FLOBASE_MODE", "self-hosted")
	if mode != "self-hosted" && mode != "cloud" {
		return errors.New("FLOBASE_MODE must be self-hosted or cloud")
	}
	token := os.Getenv("FLOBASE_API_TOKEN")
	if len(token) < 16 {
		return errors.New("FLOBASE_API_TOKEN must be set to at least 16 characters")
	}
	markup, err := strconv.ParseFloat(env("FLOBASE_MARKUP", "1.6"), 64)
	if err != nil {
		return errors.New("FLOBASE_MARKUP must be a number such as 1.6")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, env("FLOBASE_DATABASE_URL", "postgres://flobase:flobase@localhost:5433/flobase"))
	if err != nil {
		return err
	}
	defer st.Close()

	// Other regions are region servers added in the dashboard.
	if err := st.SeedRegions(ctx, []catalog.Region{catalog.LocalRegion}); err != nil {
		return err
	}
	if err := st.FailInterrupted(ctx); err != nil {
		return err
	}

	// In Docker Compose the control plane and gateway join each project's
	// network to reach its services by name.
	var attach []string
	for _, name := range strings.Split(os.Getenv("FLOBASE_ATTACH"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			attach = append(attach, name)
		}
	}
	// Off-site backup copies are on when a bucket and keys are set.
	var offsite *provisioner.Offsite
	if os.Getenv("FLOBASE_BACKUP_S3_BUCKET") != "" {
		offsite = &provisioner.Offsite{
			Endpoint:      env("FLOBASE_BACKUP_S3_ENDPOINT", "https://s3.amazonaws.com"),
			Region:        env("FLOBASE_BACKUP_S3_REGION", "us-east-1"),
			Bucket:        os.Getenv("FLOBASE_BACKUP_S3_BUCKET"),
			Prefix:        os.Getenv("FLOBASE_BACKUP_S3_PREFIX"),
			AccessKey:     os.Getenv("FLOBASE_BACKUP_S3_ACCESS_KEY"),
			SecretKey:     os.Getenv("FLOBASE_BACKUP_S3_SECRET_KEY"),
			EncryptionKey: os.Getenv("FLOBASE_BACKUP_ENCRYPTION_KEY"),
		}
		if offsite.AccessKey == "" || offsite.SecretKey == "" {
			return errors.New("FLOBASE_BACKUP_S3_BUCKET is set, so FLOBASE_BACKUP_S3_ACCESS_KEY and FLOBASE_BACKUP_S3_SECRET_KEY must be too")
		}
	}
	docker := provisioner.NewDocker(env("FLOBASE_DOCKER_SOCKET", "/var/run/docker.sock"), env("FLOBASE_PUBLIC_HOST", "localhost"), attach, nil)
	if err := docker.Ping(ctx); err != nil {
		return errors.New("cannot reach Docker at FLOBASE_DOCKER_SOCKET: " + err.Error())
	}
	projects, err := st.Projects(ctx)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if p.Status == store.StatusActive && p.Region == provisioner.LocalRegion {
			if err := docker.Attach(ctx, p.Ref); err != nil {
				log.Warn("attach to project network", "ref", p.Ref, "err", err)
			}
		}
	}

	cfg := api.Config{
		Mode: mode, Version: version, APIToken: token, Markup: markup,
		GatewayURL: env("FLOBASE_GATEWAY_URL", "http://localhost:28471"),
		UpdateRepo: env("FLOBASE_UPDATE_REPO", "flomo-abel-231/flobase"),
		AutoUpdate: os.Getenv("FLOBASE_AUTO_UPDATE") == "true",
		InstallDir: os.Getenv("FLOBASE_INSTALL_DIR"),
		Local:      os.Getenv("FLOBASE_INSTALL_KIND") == "local",
		OwnerEmail: strings.TrimSpace(os.Getenv("FLOBASE_OWNER_EMAIL")),
	}
	// The updater needs the install's Compose project and container names,
	// which Compose put on this container.
	self := "flobase-control-plane"
	if len(attach) > 0 {
		self = attach[0]
	} else if h, err := os.Hostname(); err == nil {
		self = h
	}
	if in, err := docker.SelfInstall(ctx, self); err == nil {
		cfg.Project, cfg.Name = in.Project, in.Name
		// FloBase Desktop and the flobase CLI up to 0.3.0 wrote
		// FLOBASE_AUTO_UPDATE=false and no install directory, and their
		// compose file sets no FLOBASE_INSTALL_KIND: they update by
		// themselves now, from the directory Compose ran in.
		if in.Project == "flobase-local" {
			cfg.Local = true
		}
		if cfg.Local && cfg.InstallDir == "" {
			cfg.InstallDir, cfg.AutoUpdate = in.Dir, true
		}
	} else {
		log.Info("not started by Docker Compose; updates are by hand", "err", err)
	}
	cfg.EnvOffsite = offsite
	// Each project's calls go to its region's Docker host.
	router := provisioner.NewRouter(docker, func(ctx context.Context, ref string) (string, error) {
		p, err := st.Project(ctx, ref)
		return p.Region, err
	})
	srv := api.New(cfg, st, router, log)
	if err := srv.StartRegions(ctx); err != nil {
		return err
	}
	if err := srv.StartTLS(ctx); err != nil {
		return err
	}
	srv.StartFirewall(ctx)
	deleting, err := st.Deleting(ctx)
	if err != nil {
		return err
	}
	for _, p := range deleting {
		go srv.Teardown(p.Ref)
	}
	// Point off-site copies at .env's location or the dashboard's active one.
	go srv.ApplyStorage(ctx)
	go srv.RunBackups(ctx)
	go srv.RunUpdateChecks(ctx)
	go srv.RunAutomations(ctx)

	handler := srv.Routes()
	if dir := os.Getenv("FLOBASE_DASHBOARD_DIR"); dir != "" {
		handler = withDashboard(handler, dir)
	}

	addr := env("FLOBASE_ADDR", ":28470")
	httpServer := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- httpServer.ListenAndServe() }()
	log.Info("FloBase control plane listening", "addr", addr, "mode", mode, "version", version)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdown)
}

// withDashboard serves the single-page dashboard, falling back to
// index.html for client-side routes.
func withDashboard(apiHandler http.Handler, dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		path := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		files.ServeHTTP(w, r)
	})
}
