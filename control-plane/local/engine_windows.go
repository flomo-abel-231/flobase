package local

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Windows: FloBase uses Docker Desktop with the WSL 2 backend, installing
// both if needed. Installing WSL and Docker Desktop asks for administrator
// rights (UAC), and WSL needs a restart the first time.

var (
	dockerDesktopExe = filepath.Join(os.Getenv("ProgramFiles"), "Docker", "Docker", "Docker Desktop.exe")
	dockerDesktopBin = filepath.Join(os.Getenv("ProgramFiles"), "Docker", "Docker", "resources", "bin")
)

// useOwnEngine finds Docker Desktop's CLI when it is installed but not yet
// on this process's PATH (just installed, or PATH not refreshed).
func (m *Manager) useOwnEngine() {
	if _, err := exec.LookPath("docker"); err == nil {
		return
	}
	if _, err := os.Stat(filepath.Join(dockerDesktopBin, "docker.exe")); err == nil {
		m.dockerEnv = []string{"PATH=" + dockerDesktopBin + ";" + os.Getenv("PATH")}
		m.dockerExe = filepath.Join(dockerDesktopBin, "docker.exe")
	}
}

func (m *Manager) dockerPath() string {
	if m.dockerExe != "" {
		return m.dockerExe
	}
	return "docker"
}

// runAdmin runs a program with administrator rights, showing the UAC
// prompt, and waits for it.
func runAdmin(ctx context.Context, program string, args ...string) error {
	list := ""
	for i, a := range args {
		if i > 0 {
			list += ","
		}
		list += "'" + a + "'"
	}
	script := "$p = Start-Process -FilePath '" + program + "' -Verb RunAs -Wait -PassThru"
	if list != "" {
		script = "$p = Start-Process -FilePath '" + program + "' -ArgumentList " + list + " -Verb RunAs -Wait -PassThru"
	}
	script += "; exit $p.ExitCode"
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	hideWindow(cmd)
	return cmd.Run()
}

func (m *Manager) startOrInstallEngine(ctx context.Context) error {
	if _, err := os.Stat(dockerDesktopExe); errors.Is(err, os.ErrNotExist) {
		if err := m.installDockerDesktop(ctx); err != nil {
			return err
		}
		m.useOwnEngine()
	}
	m.step("Starting Docker Desktop (the first start can take a few minutes; accept its terms if it asks)")
	if err := exec.Command(dockerDesktopExe).Start(); err != nil {
		return err
	}
	if !m.waitEngine(ctx, 6*time.Minute) {
		return &ErrNoDocker{Detail: "Docker Desktop did not start; open it once, accept its terms, then try again"}
	}
	return nil
}

func (m *Manager) installDockerDesktop(ctx context.Context) error {
	wsl := exec.CommandContext(ctx, "wsl.exe", "--status")
	hideWindow(wsl)
	if err := wsl.Run(); err != nil {
		if m.Confirm != nil && !m.Confirm("FloBase needs WSL 2. Install it now? Windows asks for administrator rights and a restart.") {
			return &ErrNoDocker{Detail: "WSL 2 is not installed"}
		}
		m.step("Installing WSL 2")
		if err := runAdmin(ctx, "wsl.exe", "--install", "--no-distribution"); err != nil {
			return err
		}
		return &ErrRestartRequired{What: "WSL 2 is installed. Restart Windows, then start FloBase again to finish setting up."}
	}
	if _, err := exec.LookPath("winget"); err != nil {
		return &ErrNoDocker{Detail: "winget is missing; install App Installer from the Microsoft Store, or Docker Desktop by hand"}
	}
	if m.Confirm != nil && !m.Confirm("FloBase needs Docker Desktop. Install it now? It is free for personal use, education, open source and businesses under 250 people and $10M revenue; larger companies need a paid Docker subscription.") {
		return &ErrNoDocker{Detail: "Docker Desktop is not installed"}
	}
	m.step("Installing Docker Desktop with winget (Windows asks for administrator rights)")
	cmd := exec.CommandContext(ctx, "winget", "install", "--exact", "--id", "Docker.DockerDesktop",
		"--silent", "--accept-source-agreements", "--accept-package-agreements")
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return &ErrNoDocker{Detail: "installing Docker Desktop failed: " + string(out)}
	}
	return nil
}

// Docker Desktop is the user's, not FloBase's: leave it running.
func (m *Manager) stopOwnEngine(context.Context) error { return nil }

func (m *Manager) removeOwnEngine(context.Context) {}
