package provisioner

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// updaterImage runs upgrade.sh: it has the Docker CLI and Compose plugin.
const updaterImage = "docker:27.5.1-cli"

// Install is the Compose install a control plane belongs to, read from the
// labels Compose puts on its own container.
type Install struct {
	// Project is the Compose project, which names its volumes.
	Project string
	// Dir is the install directory on the Docker host.
	Dir string
	// Name prefixes the containers (FLOBASE_NAME): Name-control-plane.
	Name string
}

// SelfInstall reads the Compose labels of container (the control plane's
// own, by name or ID).
func (d *Docker) SelfInstall(ctx context.Context, container string) (Install, error) {
	var info struct {
		Name   string
		Config struct{ Labels map[string]string }
	}
	if err := d.call(ctx, "GET", "/containers/"+url.PathEscape(container)+"/json", nil, &info); err != nil {
		return Install{}, err
	}
	in := Install{
		Project: info.Config.Labels["com.docker.compose.project"],
		Dir:     info.Config.Labels["com.docker.compose.project.working_dir"],
		Name:    strings.TrimSuffix(strings.TrimPrefix(info.Name, "/"), "-control-plane"),
	}
	if in.Project == "" {
		return in, fmt.Errorf("container %s was not started by Docker Compose", container)
	}
	return in, nil
}

// UpdateJob says what the updater upgrades.
type UpdateJob struct {
	Install
	Version string
	Repo    string
	// Fetch downloads the release's upgrade.sh first when the install
	// directory has none (desktop and CLI installs made before 0.3.1).
	Fetch bool
}

func updaterName(name string) string {
	if name == "" {
		name = "flobase"
	}
	return name + "-updater"
}

// StartUpdater runs upgrade.sh in the install directory in its own
// container, so the update carries on while it replaces the control plane.
// The directory is a path on the Docker host, mounted at the same path
// inside, so Compose keeps the same project directory.
func (d *Docker) StartUpdater(ctx context.Context, job UpdateJob) error {
	name := updaterName(job.Name)
	info, err := d.inspect(ctx, name)
	if err == nil && info.State.Status == "running" {
		return fmt.Errorf("an update is already running")
	}
	if err == nil {
		_ = d.call(ctx, "DELETE", "/containers/"+name+"?force=true", nil, nil)
	}
	if err := d.pull(ctx, updaterImage); err != nil {
		return err
	}
	cmd := []string{job.Dir + "/upgrade.sh", job.Version}
	if job.Fetch {
		script := fmt.Sprintf("https://github.com/%s/releases/download/v%s/upgrade.sh", job.Repo, job.Version)
		cmd = []string{"-c", `[ -f upgrade.sh ] || wget -qO upgrade.sh "$1" || exit 1; exec sh ./upgrade.sh "$2"`, "upgrade", script, job.Version}
	}
	env := []string{"FLOBASE_INSTALL_DIR=" + job.Dir, "FLOBASE_UPDATE_REPO=" + job.Repo}
	if job.Project != "" {
		// Compose names volumes after the project: keep the install's.
		env = append(env, "COMPOSE_PROJECT_NAME="+job.Project)
	}
	config := map[string]any{
		"Image":      updaterImage,
		"Entrypoint": []string{"sh"},
		"Cmd":        cmd,
		"Env":        env,
		"WorkingDir": job.Dir,
		"Labels":     map[string]string{"dev.flobase.role": "updater"},
		"HostConfig": map[string]any{
			"Binds":         []string{"/var/run/docker.sock:/var/run/docker.sock", job.Dir + ":" + job.Dir},
			"RestartPolicy": map[string]any{"Name": "no"},
		},
	}
	if err := d.call(ctx, "POST", "/containers/create?name="+url.QueryEscape(name), config, nil); err != nil {
		return err
	}
	return d.call(ctx, "POST", "/containers/"+name+"/start", nil, nil)
}

// UpdaterRunning reports whether an update of the install whose containers
// are called name is in progress.
func (d *Docker) UpdaterRunning(ctx context.Context, name string) bool {
	info, err := d.inspect(ctx, updaterName(name))
	return err == nil && info.State.Status == "running"
}

// UpdateFailed reports whether the last update of the install whose
// containers are called name was to version and failed (upgrade.sh rolled
// back), so automatic updates do not retry it on every start.
func (d *Docker) UpdateFailed(ctx context.Context, name, version string) bool {
	var info struct {
		State struct {
			Status   string
			ExitCode int
		}
		Config struct{ Cmd []string }
	}
	if err := d.call(ctx, "GET", "/containers/"+updaterName(name)+"/json", nil, &info); err != nil {
		return false
	}
	if info.State.Status != "exited" || info.State.ExitCode == 0 || len(info.Config.Cmd) == 0 {
		return false
	}
	return info.Config.Cmd[len(info.Config.Cmd)-1] == version
}
