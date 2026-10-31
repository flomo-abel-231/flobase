package provisioner

import (
	"context"
	"fmt"
	"net/url"
)

// updaterImage runs upgrade.sh: it has the Docker CLI and Compose plugin.
const updaterImage = "docker:27.5.1-cli"

// StartUpdater runs installDir/upgrade.sh for version in its own container,
// so the update carries on while it replaces the control plane. installDir
// is a path on the Docker host, mounted at the same path inside.
func (d *Docker) StartUpdater(ctx context.Context, installDir, version, repo string) error {
	const name = "flobase-updater"
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
	config := map[string]any{
		"Image":      updaterImage,
		"Entrypoint": []string{"sh"},
		"Cmd":        []string{installDir + "/upgrade.sh", version},
		"Env":        []string{"FLOBASE_INSTALL_DIR=" + installDir, "FLOBASE_UPDATE_REPO=" + repo},
		"WorkingDir": installDir,
		"Labels":     map[string]string{"dev.flobase.role": "updater"},
		"HostConfig": map[string]any{
			"Binds":         []string{"/var/run/docker.sock:/var/run/docker.sock", installDir + ":" + installDir},
			"RestartPolicy": map[string]any{"Name": "no"},
		},
	}
	if err := d.call(ctx, "POST", "/containers/create?name="+url.QueryEscape(name), config, nil); err != nil {
		return err
	}
	return d.call(ctx, "POST", "/containers/"+name+"/start", nil, nil)
}

// UpdaterRunning reports whether an update is in progress.
func (d *Docker) UpdaterRunning(ctx context.Context) bool {
	info, err := d.inspect(ctx, "flobase-updater")
	return err == nil && info.State.Status == "running"
}
