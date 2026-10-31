// Package local runs FloBase on a developer's computer (macOS, Windows,
// Linux) on top of the Docker engine already installed there: Docker
// Desktop, OrbStack, Rancher Desktop or Docker Engine. The flobase CLI and
// FloBase Desktop both drive it.
//
// It keeps everything in ~/.flobase (FLOBASE_LOCAL_DIR overrides): the
// release's docker-compose.yml, a .env with generated secrets, and runs the
// stack as the Compose project "flobase-local", so it never shares volumes
// with a server-style install on the same machine.
package local

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultRepo = "flomo-abel-231/flobase"
	project     = "flobase-local"
)

// Manager runs one local FloBase.
type Manager struct {
	Dir  string
	Repo string
	// Version is the release this program belongs to; a dev build follows
	// the latest release.
	Version string
	// Progress, when set, receives one line per step.
	Progress func(string)
	// Confirm, when set, is asked before installing software that needs
	// the user's agreement (Docker Desktop's licence on Windows); nil
	// means yes.
	Confirm func(question string) bool

	// dockerEnv is added to every docker command: DOCKER_HOST and PATH for
	// an engine FloBase installed itself (see engine_*.go).
	dockerEnv []string
	// ownEngine is true when FloBase runs its own engine (macOS).
	ownEngine bool
	// dockerExe is the docker CLI's path when it is not on PATH (Windows).
	dockerExe string
}

// New returns a manager for ~/.flobase.
func New(version string) (*Manager, error) {
	dir := os.Getenv("FLOBASE_LOCAL_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".flobase")
	}
	return &Manager{Dir: dir, Repo: DefaultRepo, Version: version}, nil
}

func (m *Manager) step(format string, args ...any) {
	if m.Progress != nil {
		m.Progress(fmt.Sprintf(format, args...))
	}
}

// ErrNoDocker means no usable Docker engine was found; its message says
// what to install for this operating system.
type ErrNoDocker struct{ Detail string }

func (e *ErrNoDocker) Error() string {
	hint := "Install Docker Engine: https://docs.docker.com/engine/install/"
	switch runtime.GOOS {
	case "darwin":
		hint = "Install and start Docker Desktop (https://www.docker.com/products/docker-desktop/) or OrbStack (https://orbstack.dev)."
	case "windows":
		hint = "Install and start Docker Desktop (https://www.docker.com/products/docker-desktop/) with the WSL 2 backend."
	}
	return "FloBase needs a running Docker engine. " + hint + " (" + e.Detail + ")"
}

// docker runs the docker CLI and returns its combined output.
func (m *Manager) docker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, m.dockerPath(), args...)
	cmd.Env = append(os.Environ(), m.dockerEnv...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// CheckDocker makes sure a Docker engine is running, starting the one on
// this computer or installing one if there is none (see EnsureEngine).
func (m *Manager) CheckDocker(ctx context.Context) error {
	return m.EnsureEngine(ctx)
}

// engineReady reports whether docker and Compose v2 answer.
func (m *Manager) engineReady(ctx context.Context) bool {
	if _, err := m.docker(ctx, "info", "--format", "{{.ServerVersion}}"); err != nil {
		return false
	}
	_, err := m.docker(ctx, "compose", "version")
	return err == nil
}

func (m *Manager) compose(ctx context.Context, args ...string) (string, error) {
	base := []string{"compose", "-p", project, "--project-directory", m.Dir, "-f", filepath.Join(m.Dir, "docker-compose.yml")}
	return m.docker(ctx, append(base, args...)...)
}

// --- .env --------------------------------------------------------------

func (m *Manager) envPath() string { return filepath.Join(m.Dir, ".env") }

// Env reads the local .env.
func (m *Manager) Env() map[string]string {
	env := map[string]string{}
	f, err := os.Open(m.envPath())
	if err != nil {
		return env
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
			env[k] = v
		}
	}
	return env
}

// setEnv writes keys into .env, replacing existing lines and keeping the
// rest, comments included.
func (m *Manager) setEnv(values map[string]string) error {
	raw, _ := os.ReadFile(m.envPath())
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	done := map[string]bool{}
	for i, line := range lines {
		if k, _, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			if v, set := values[k]; set {
				lines[i] = k + "=" + v
				done[k] = true
			}
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !done[k] {
			lines = append(lines, k+"="+values[k])
		}
	}
	return os.WriteFile(m.envPath(), []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func secret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- releases ----------------------------------------------------------

var httpClient = &http.Client{Timeout: 60 * time.Second}

// LatestRelease asks GitHub for the newest published release.
func (m *Manager) LatestRelease(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+m.Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("check for releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("check for releases: GitHub answered %s", resp.Status)
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return strings.TrimPrefix(rel.Tag, "v"), nil
}

func (m *Manager) download(ctx context.Context, version, name, dest string) error {
	url := fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", m.Repo, version, name)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s from release v%s: %s", name, version, resp.Status)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// stackVersion picks the release to run: this program's own release, or
// the latest one for a development build.
func (m *Manager) stackVersion(ctx context.Context) (string, error) {
	if isRelease(m.Version) {
		return m.Version, nil
	}
	return m.LatestRelease(ctx)
}

func isRelease(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}

// Newer reports whether release a is later than b.
func Newer(a, b string) bool {
	if !isRelease(a) || !isRelease(b) {
		return false
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range pa {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x > y
		}
	}
	return false
}

// install puts a release's compose file and settings template in Dir and
// writes .env on first use.
func (m *Manager) install(ctx context.Context, version string) error {
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return err
	}
	m.step("Downloading FloBase %s", version)
	if err := m.download(ctx, version, "docker-compose.yml", filepath.Join(m.Dir, "docker-compose.yml")); err != nil {
		return err
	}
	if err := m.download(ctx, version, "env.example", filepath.Join(m.Dir, "env.example")); err != nil {
		return err
	}
	if _, err := os.Stat(m.envPath()); errors.Is(err, os.ErrNotExist) {
		m.step("Generating secrets")
		tmpl, err := os.ReadFile(filepath.Join(m.Dir, "env.example"))
		if err != nil {
			return err
		}
		if err := os.WriteFile(m.envPath(), tmpl, 0o600); err != nil {
			return err
		}
		if err := m.setEnv(map[string]string{
			"FLOBASE_API_TOKEN": secret(), "FLOBASE_PLATFORM_DB_PASSWORD": secret(),
			"FLOBASE_PUBLIC_HOST": "localhost", "FLOBASE_GATEWAY_URL": "http://localhost:8000",
			"FLOBASE_PORT": "8080", "FLOBASE_GATEWAY_PORT": "8000",
			// The desktop app and CLI update the stack themselves.
			"FLOBASE_AUTO_UPDATE": "false", "FLOBASE_INSTALL_DIR": "",
		}); err != nil {
			return err
		}
	}
	return m.setEnv(map[string]string{"FLOBASE_VERSION": version})
}

// --- lifecycle -----------------------------------------------------------

// Status is what the CLI and the desktop app show.
type Status struct {
	Installed    bool   `json:"installed"`
	Running      bool   `json:"running"`
	Version      string `json:"version"`
	DashboardURL string `json:"dashboard_url"`
	GatewayURL   string `json:"gateway_url"`
	Dir          string `json:"dir"`
}

func (m *Manager) port() string {
	if p := m.Env()["FLOBASE_PORT"]; p != "" {
		return p
	}
	return "8080"
}

// DashboardURL is where the dashboard answers. With signIn, the API token
// rides in the URL fragment, which browsers never send to the server, so
// the dashboard signs in without asking.
func (m *Manager) DashboardURL(signIn bool) string {
	u := "http://localhost:" + m.port() + "/"
	if signIn {
		if t := m.Env()["FLOBASE_API_TOKEN"]; t != "" {
			u += "#token=" + t
		}
	}
	return u
}

func (m *Manager) healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:"+m.port()+"/api/v1/health", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// otherStack names another FloBase using this Docker engine (a server
// install, or a source checkout's `make up`), which holds the same
// container names and ports; "" when there is none.
func (m *Manager) otherStack(ctx context.Context) string {
	out, err := m.docker(ctx, "inspect", "-f", `{{index .Config.Labels "com.docker.compose.project"}}|{{.State.Running}}`, "flobase-control-plane")
	if err != nil {
		return ""
	}
	proj, running, _ := strings.Cut(strings.TrimSpace(out), "|")
	if proj == project || running != "true" {
		return ""
	}
	return proj
}

func (m *Manager) Status(ctx context.Context) Status {
	env := m.Env()
	return Status{
		Installed:    env["FLOBASE_API_TOKEN"] != "",
		Running:      m.otherStack(ctx) == "" && m.healthy(ctx),
		Version:      env["FLOBASE_VERSION"],
		DashboardURL: m.DashboardURL(false),
		GatewayURL:   env["FLOBASE_GATEWAY_URL"],
		Dir:          m.Dir,
	}
}

func (m *Manager) waitHealthy(ctx context.Context) error {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if m.healthy(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	logs, _ := m.compose(ctx, "logs", "--tail", "20", "flobase")
	return fmt.Errorf("FloBase did not start within 3 minutes:\n%s", logs)
}

// Start sets FloBase up on first use, starts it and the project databases
// that were running when it stopped, and waits until it answers.
func (m *Manager) Start(ctx context.Context) error {
	m.step("Checking Docker")
	if err := m.CheckDocker(ctx); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "docker-compose.yml")); errors.Is(err, os.ErrNotExist) || !m.Status(ctx).Installed {
		version, err := m.stackVersion(ctx)
		if err != nil {
			return err
		}
		if err := m.install(ctx, version); err != nil {
			return err
		}
	}
	if other := m.otherStack(ctx); other != "" {
		return fmt.Errorf("another FloBase (Compose project %q) is running on this Docker engine and uses the same ports; stop it first with: docker compose -p %s stop", other, other)
	}
	// A stopped container left by another FloBase would block the names.
	if out, err := m.docker(ctx, "inspect", "-f", `{{index .Config.Labels "com.docker.compose.project"}}`, "flobase-control-plane"); err == nil && strings.TrimSpace(out) != project {
		_, _ = m.docker(ctx, "rm", "flobase-control-plane", "flobase-gateway")
	}
	if out, _ := m.docker(ctx, "image", "ls", "-q", "ghcr.io/flomo-abel-231/flobase"); strings.TrimSpace(out) == "" {
		m.step("Starting FloBase %s (downloading about 1 GB the first time)", m.Env()["FLOBASE_VERSION"])
	} else {
		m.step("Starting FloBase %s", m.Env()["FLOBASE_VERSION"])
	}
	if _, err := m.compose(ctx, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	m.step("Waiting for the dashboard")
	if err := m.waitHealthy(ctx); err != nil {
		return err
	}
	refs, err := m.projectRefs(ctx)
	if err == nil && len(refs) > 0 {
		m.step("Starting %d project database(s)", len(refs))
		var started []string
		for _, ref := range refs {
			ids, _ := m.docker(ctx, "ps", "-aq", "--filter", "label=dev.flobase.project="+ref)
			if ids = strings.TrimSpace(ids); ids != "" {
				_, _ = m.docker(ctx, append([]string{"start"}, strings.Fields(ids)...)...)
				started = append(started, strings.Fields(ids)...)
			}
		}
		m.waitContainersHealthy(ctx, started)
	}
	m.step("FloBase is running at %s", m.DashboardURL(false))
	return nil
}

// waitContainersHealthy waits up to two minutes for containers with a
// health check to report healthy, so a project answers once start returns.
func (m *Manager) waitContainersHealthy(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		out, err := m.docker(ctx, append([]string{"inspect", "-f", "{{if .State.Health}}{{.State.Health.Status}}{{else}}healthy{{end}}"}, ids...)...)
		if err == nil && !strings.Contains(out, "starting") && !strings.Contains(out, "unhealthy") {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// projectRefs asks the local control plane which projects it runs.
func (m *Manager) projectRefs(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:"+m.port()+"/api/v1/projects", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+m.Env()["FLOBASE_API_TOKEN"])
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var list []struct {
		Ref string `json:"ref"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	refs := make([]string, 0, len(list))
	for _, p := range list {
		refs = append(refs, p.Ref)
	}
	return refs, nil
}

// Stop stops the project databases and FloBase, keeping all data.
func (m *Manager) Stop(ctx context.Context) error {
	if err := m.CheckDocker(ctx); err != nil {
		return err
	}
	if refs, err := m.projectRefs(ctx); err == nil && len(refs) > 0 {
		m.step("Stopping %d project database(s)", len(refs))
		for _, ref := range refs {
			ids, _ := m.docker(ctx, "ps", "-q", "--filter", "label=dev.flobase.project="+ref)
			if ids = strings.TrimSpace(ids); ids != "" {
				_, _ = m.docker(ctx, append([]string{"stop"}, strings.Fields(ids)...)...)
			}
		}
	}
	m.step("Stopping FloBase")
	if _, err := m.compose(ctx, "stop"); err != nil {
		return err
	}
	// A Docker engine FloBase installed itself is FloBase's alone: stop it
	// too, to give its memory back.
	return m.stopOwnEngine(ctx)
}

// Logs returns the control plane's recent output.
func (m *Manager) Logs(ctx context.Context, lines int) (string, error) {
	return m.compose(ctx, "logs", "--no-color", "--tail", strconv.Itoa(lines), "flobase", "gateway")
}

// Update moves the local stack to the latest release if there is a newer
// one, and reports the version it ends on.
func (m *Manager) Update(ctx context.Context) (string, error) {
	if err := m.CheckDocker(ctx); err != nil {
		return "", err
	}
	current := m.Env()["FLOBASE_VERSION"]
	latest, err := m.LatestRelease(ctx)
	if err != nil {
		return current, err
	}
	if !Newer(latest, current) {
		m.step("FloBase %s is the latest release", current)
		return current, nil
	}
	if err := m.install(ctx, latest); err != nil {
		return current, err
	}
	m.step("Updating FloBase %s to %s", current, latest)
	if _, err := m.compose(ctx, "pull", "--quiet"); err != nil {
		return current, err
	}
	if _, err := m.compose(ctx, "up", "-d", "--remove-orphans"); err != nil {
		return current, err
	}
	if err := m.waitHealthy(ctx); err != nil {
		return latest, err
	}
	m.step("FloBase is now %s", latest)
	return latest, nil
}

// Uninstall removes FloBase. With everything, it also deletes every
// project database, all data and ~/.flobase.
func (m *Manager) Uninstall(ctx context.Context, everything bool) error {
	if err := m.CheckDocker(ctx); err != nil {
		return err
	}
	if everything {
		if refs, err := m.projectRefs(ctx); err == nil {
			m.step("Deleting %d project(s)", len(refs))
			for _, ref := range refs {
				filter := "label=dev.flobase.project=" + ref
				if ids, _ := m.docker(ctx, "ps", "-aq", "--filter", filter); strings.TrimSpace(ids) != "" {
					_, _ = m.docker(ctx, append([]string{"rm", "-f"}, strings.Fields(ids)...)...)
				}
				if vols, _ := m.docker(ctx, "volume", "ls", "-q", "--filter", filter); strings.TrimSpace(vols) != "" {
					_, _ = m.docker(ctx, append([]string{"volume", "rm", "-f"}, strings.Fields(vols)...)...)
				}
				_, _ = m.docker(ctx, "network", "rm", "flobase-"+ref)
			}
		}
	}
	m.step("Removing FloBase")
	args := []string{"down", "--remove-orphans"}
	if everything {
		args = append(args, "--volumes")
	}
	// Set up but never started: there is nothing for Compose to remove.
	if _, err := os.Stat(filepath.Join(m.Dir, "docker-compose.yml")); err == nil {
		if _, err := m.compose(ctx, args...); err != nil {
			return err
		}
	}
	if everything {
		m.removeOwnEngine(ctx)
		return os.RemoveAll(m.Dir)
	}
	return m.stopOwnEngine(ctx)
}

// OpenURL opens a URL in the default browser.
func OpenURL(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	hideWindow(cmd)
	return cmd.Start()
}
