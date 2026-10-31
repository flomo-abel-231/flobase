package local

import (
	"context"
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
// framework (macOS 13+), in ~/.flobase/runtime. Versions are pinned and
// checksums verified.
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
	m.dockerEnv = []string{
		"PATH=" + filepath.Join(rt, "bin") + ":" + os.Getenv("PATH"),
		"COLIMA_HOME=" + filepath.Join(rt, "colima"),
		"DOCKER_CONFIG=" + filepath.Join(rt, "docker-config"),
		"DOCKER_HOST=unix://" + filepath.Join(rt, "colima", colimaProfile, "docker.sock"),
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
	m.step("Starting FloBase's Docker engine (%d CPUs, %d GB); the first start takes a few minutes", cpus, memGB)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	return m.colima(ctx, "start", "--vm-type", "vz", "--mount-type", "virtiofs",
		"--cpu", strconv.Itoa(cpus), "--memory", strconv.Itoa(memGB), "--disk", "60")
}

func (m *Manager) stopOwnEngine(ctx context.Context) error {
	if !m.ownEngine {
		return nil
	}
	m.step("Stopping FloBase's Docker engine")
	return m.colima(ctx, "stop")
}

// removeOwnEngine deletes FloBase's VM before its files go.
func (m *Manager) removeOwnEngine(ctx context.Context) {
	if m.ownEngine {
		_ = m.colima(ctx, "delete", "--force")
	}
}
