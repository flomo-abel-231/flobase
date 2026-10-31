package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// macOS: FloBase's own engine is Colima on Lima with Apple's Virtualization
// framework (macOS 13+). The programs live in ~/.flobase/runtime; Colima
// keeps the VM itself in ~/.colima/flobase, its fixed location. Versions
// are pinned and checksums verified.
const (
	colimaVersion  = "0.10.3"
	limaVersion    = "2.2.1"
	dockerVersion  = "29.9.1"
	composeVersion = "5.6.0"
	colimaProfile  = "flobase"
)

func (m *Manager) runtimeDir() string { return filepath.Join(m.Dir, "runtime") }

// useOwnEngine points docker commands at FloBase's own engine once it has
// been installed.
func (m *Manager) useOwnEngine() {
	rt := m.runtimeDir()
	if _, err := os.Stat(filepath.Join(rt, "bin", "colima")); err != nil {
		return
	}
	m.ownEngine = true
	home, _ := os.UserHomeDir()
	m.setOwnEngineEnv("unix://" + filepath.Join(home, ".colima", colimaProfile, "docker.sock"))
}

func (m *Manager) setOwnEngineEnv(dockerHost string) {
	rt := m.runtimeDir()
	m.dockerEnv = []string{
		"PATH=" + filepath.Join(rt, "bin") + ":" + os.Getenv("PATH"),
		"DOCKER_CONFIG=" + filepath.Join(rt, "docker-config"),
		"DOCKER_HOST=" + dockerHost,
	}
}

// refreshSocket asks Colima where its Docker socket is, rather than
// assuming the default location.
func (m *Manager) refreshSocket(ctx context.Context) {
	cmd := exec.CommandContext(ctx, filepath.Join(m.runtimeDir(), "bin", "colima"), "status", "--profile", colimaProfile, "--json")
	cmd.Env = append(os.Environ(), m.dockerEnv...)
	out, err := cmd.Output()
	if err != nil {
		return
	}
	var st struct {
		DockerSocket string `json:"docker_socket"`
	}
	if json.Unmarshal(out, &st) == nil && st.DockerSocket != "" {
		m.setOwnEngineEnv(st.DockerSocket)
	}
}

func (m *Manager) dockerPath() string {
	if m.ownEngine {
		return filepath.Join(m.runtimeDir(), "bin", "docker")
	}
	return "docker"
}

func (m *Manager) startOrInstallEngine(ctx context.Context) error {
	if !m.ownEngine && !bundledOnly() {
		// An engine that is installed but not running: start it.
		for _, app := range []string{"Docker", "OrbStack"} {
			if _, err := os.Stat("/Applications/" + app + ".app"); err == nil {
				m.step("Starting %s", app)
				if err := exec.Command("open", "-a", app).Run(); err == nil && m.waitEngine(ctx, 3*time.Minute) {
					return nil
				}
			}
		}
	}
	if !m.ownEngine {
		if err := m.installOwnEngine(ctx); err != nil {
			return err
		}
		m.useOwnEngine()
	}
	return m.startOwnEngine(ctx)
}

func (m *Manager) installOwnEngine(ctx context.Context) error {
	out, _ := exec.Command("sw_vers", "-productVersion").Output()
	if major, _ := strconv.Atoi(strings.Split(strings.TrimSpace(string(out)), ".")[0]); major > 0 && major < 13 {
		return &ErrNoDocker{Detail: "FloBase's own engine needs macOS 13 or later; on this Mac install Docker Desktop instead"}
	}
	rt := m.runtimeDir()
	arch, dockerArch, composeArch := "x86_64", "x86_64", "x86_64"
	if runtime.GOARCH == "arm64" {
		arch, dockerArch, composeArch = "arm64", "aarch64", "aarch64"
	}
	m.step("Installing a Docker engine for FloBase (Colima %s, Docker %s); this happens once", colimaVersion, dockerVersion)

	colimaURL := fmt.Sprintf("https://github.com/abiosoft/colima/releases/download/v%s/colima-Darwin-%s", colimaVersion, arch)
	sums, err := fetchText(ctx, colimaURL+".sha256sum")
	if err != nil {
		return err
	}
	if err := fetch(ctx, colimaURL, filepath.Join(rt, "bin", "colima"), sumFor(sums, "colima-Darwin-"+arch)); err != nil {
		return err
	}

	limaName := fmt.Sprintf("lima-%s-Darwin-%s.tar.gz", limaVersion, arch)
	limaBase := fmt.Sprintf("https://github.com/lima-vm/lima/releases/download/v%s/", limaVersion)
	if sums, err = fetchText(ctx, limaBase+"SHA256SUMS"); err != nil {
		return err
	}
	limaTar := filepath.Join(rt, limaName)
	if err := fetch(ctx, limaBase+limaName, limaTar, sumFor(sums, limaName)); err != nil {
		return err
	}
	if err := untar(limaTar, rt, nil); err != nil {
		return err
	}
	os.Remove(limaTar)

	// Docker publishes no checksums for its static macOS CLI; it comes
	// over HTTPS from download.docker.com.
	dockerTar := filepath.Join(rt, "docker.tgz")
	if err := fetch(ctx, fmt.Sprintf("https://download.docker.com/mac/static/stable/%s/docker-%s.tgz", dockerArch, dockerVersion), dockerTar, ""); err != nil {
		return err
	}
	if err := untar(dockerTar, filepath.Join(rt, "bin"), func(name string) string {
		if name == "docker/docker" {
			return "docker"
		}
		return ""
	}); err != nil {
		return err
	}
	os.Remove(dockerTar)

	composeURL := fmt.Sprintf("https://github.com/docker/compose/releases/download/v%s/docker-compose-darwin-%s", composeVersion, composeArch)
	if sums, err = fetchText(ctx, composeURL+".sha256"); err != nil {
		return err
	}
	if err := fetch(ctx, composeURL, filepath.Join(rt, "docker-config", "cli-plugins", "docker-compose"), sumFor(sums, "")); err != nil {
		return err
	}
	for _, f := range []string{"bin/colima", "bin/docker", "bin/limactl", "docker-config/cli-plugins/docker-compose"} {
		if err := os.Chmod(filepath.Join(rt, f), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) colima(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, filepath.Join(m.runtimeDir(), "bin", "colima"), append(args, "--profile", colimaProfile)...)
	cmd.Env = append(os.Environ(), m.dockerEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return fmt.Errorf("colima %s: %w: %s", args[0], err, strings.Join(lines[max(0, len(lines)-5):], "\n"))
	}
	return nil
}

func (m *Manager) startOwnEngine(ctx context.Context) error {
	cpus := min(4, max(2, runtime.NumCPU()/2))
	memGB := 4
	if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		if total, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil && total >= 16<<30 {
			memGB = 6
		}
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".colima", colimaProfile)); err != nil {
		m.step("Creating FloBase's Docker engine (%d CPUs, %d GB); this takes a few minutes once", cpus, memGB)
	} else {
		m.step("Starting FloBase's Docker engine")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	if err := m.colima(ctx, "start", "--vm-type", "vz", "--mount-type", "virtiofs",
		"--cpu", strconv.Itoa(cpus), "--memory", strconv.Itoa(memGB), "--disk", "60"); err != nil {
		return err
	}
	m.refreshSocket(ctx)
	if !m.waitEngine(ctx, 2*time.Minute) {
		return &ErrNoDocker{Detail: "FloBase's Docker engine started but does not answer; run `flobase logs` or delete ~/.flobase/runtime to reinstall it"}
	}
	return nil
}

func (m *Manager) stopOwnEngine(ctx context.Context) error {
	if !m.ownEngine {
		return nil
	}
	m.step("Stopping FloBase's Docker engine")
	return m.colima(ctx, "stop")
}

// removeOwnEngine deletes FloBase's VM and its data disk before FloBase's
// files go, and ~/.colima too when no other Colima profile uses it.
func (m *Manager) removeOwnEngine(ctx context.Context) {
	if !m.ownEngine {
		return
	}
	_ = m.colima(ctx, "delete", "--force", "--data")
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".colima")
	inUse := func(path string) bool {
		entries, _ := os.ReadDir(path)
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "_") && e.Name() != "ssh_config" {
				return true // another profile or Lima instance
			}
		}
		return false
	}
	disks, _ := os.ReadDir(filepath.Join(dir, "_lima", "_disks"))
	if !inUse(dir) && !inUse(filepath.Join(dir, "_lima")) && len(disks) == 0 {
		_ = os.RemoveAll(dir)
		// Colima's download cache of the VM image, about 350 MB.
		_ = os.RemoveAll(filepath.Join(home, "Library", "Caches", "colima"))
	}
}
