package local

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Linux: FloBase uses Docker Engine, installing it with Docker's official
// script through sudo, which asks for the user's password.

func (m *Manager) useOwnEngine() {}

func (m *Manager) dockerPath() string { return "docker" }

func sudo(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func (m *Manager) startOrInstallEngine(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err != nil {
		if m.Confirm != nil && !m.Confirm("FloBase needs Docker Engine. Install it now with Docker's script (asks for your sudo password)?") {
			return &ErrNoDocker{Detail: "Docker Engine is not installed"}
		}
		m.step("Installing Docker Engine")
		if err := sudo(ctx, "sh", "-c", "curl -fsSL https://get.docker.com | sh"); err != nil {
			return err
		}
	}
	m.step("Starting Docker Engine")
	_ = sudo(ctx, "systemctl", "enable", "--now", "docker")
	if m.waitEngine(ctx, time.Minute) {
		return nil
	}
	// Running but not reachable as this user: join the docker group.
	if _, err := m.docker(ctx, "info"); err != nil && strings.Contains(err.Error(), "permission denied") {
		user := os.Getenv("USER")
		if user == "" {
			return errors.New("add yourself to the docker group, then sign in again")
		}
		if err := sudo(ctx, "usermod", "-aG", "docker", user); err != nil {
			return err
		}
		return &ErrRestartRequired{What: "You were added to the docker group. Sign out and back in, then start FloBase again."}
	}
	return &ErrNoDocker{Detail: "Docker Engine did not start"}
}

func (m *Manager) stopOwnEngine(context.Context) error { return nil }

func (m *Manager) removeOwnEngine(context.Context) {}
