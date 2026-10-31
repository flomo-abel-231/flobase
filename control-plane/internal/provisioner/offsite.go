package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"flobase/control-plane/internal/keys"
)

// Off-site copies: a small rclone container per Postgres project copies its
// backups volume to S3-compatible storage every 30 seconds, optionally
// encrypted on this machine first. Copies outlive the project, so a fresh
// FloBase pointed at the same bucket can restore projects after losing the
// machine.
//
// Layout under the bucket and prefix, one folder per project ref:
//
//	<ref>/meta.json       name, engine and plan when the project was made
//	<ref>/base/<name>/    full backups, as on disk
//	<ref>/wal/            archived changes
//	<ref>/config/         the database's config volume (pgsodium key included)

const rcloneImage = "rclone/rclone:1.75.1"

// Offsite is where copies go. Any S3-compatible service works: AWS S3,
// Cloudflare R2, Backblaze B2, Wasabi, MinIO.
type Offsite struct {
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretKey string
	// EncryptionKey, when set, encrypts file contents and names with rclone
	// crypt before upload. Losing it makes the copies unreadable.
	EncryptionKey string
}

// OffsiteProject is a project found in off-site storage.
type OffsiteProject struct {
	Ref     string         `json:"ref"`
	Meta    map[string]any `json:"meta"`
	Backups []Backup       `json:"backups"`
	// LastChange is when the newest archived change was copied.
	LastChange *time.Time `json:"last_change,omitempty"`
}

func (o *Offsite) env() []string {
	path := o.Bucket + "/"
	if p := strings.Trim(o.Prefix, "/"); p != "" {
		path += p + "/"
	}
	env := []string{
		"RCLONE_CONFIG_S3_TYPE=s3", "RCLONE_CONFIG_S3_PROVIDER=Other",
		"RCLONE_CONFIG_S3_ENDPOINT=" + o.Endpoint, "RCLONE_CONFIG_S3_REGION=" + o.Region,
		"RCLONE_CONFIG_S3_ACCESS_KEY_ID=" + o.AccessKey, "RCLONE_CONFIG_S3_SECRET_ACCESS_KEY=" + o.SecretKey,
		"PLAIN_ROOT=s3:" + path,
	}
	if o.EncryptionKey != "" {
		env = append(env, "RCLONE_CONFIG_SAFE_TYPE=crypt", "RCLONE_CONFIG_SAFE_REMOTE=s3:"+strings.TrimSuffix(path, "/"),
			"ENC_KEY="+o.EncryptionKey)
	}
	return env
}

// rclonePrelude sets ROOT, the folder that holds one folder per project.
const rclonePrelude = `set -e
if [ -n "$ENC_KEY" ]; then
  export RCLONE_CONFIG_SAFE_PASSWORD="$(rclone obscure "$ENC_KEY")"
  ROOT="safe:"
else
  ROOT="$PLAIN_ROOT"
fi
`

const shipperScript = rclonePrelude + `D="${ROOT}$REF"
printf '%s' "$META" | rclone rcat "$D/meta.json" || echo "could not write meta.json"
while true; do
  if rclone copy /backups "$D" -q --exclude '*.part' --exclude '*.part/**' \
     && rclone copy /config "$D/config" -q; then
    echo "synced $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  else
    echo "sync failed $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  fi
  sleep 30
done`

// EnsureOffsite starts the project's sync container unless it runs already
// or off-site storage is off. meta is stored next to the copies.
func (d *Docker) EnsureOffsite(ctx context.Context, ref string, meta map[string]any) error {
	if d.offsite == nil {
		return nil
	}
	name := "flobase-" + ref + "-offsite"
	if _, err := d.inspect(ctx, name); err == nil {
		return nil
	} else if !isNotFound(err) {
		return err
	}
	if err := d.pull(ctx, rcloneImage); err != nil {
		return err
	}
	b, _ := json.Marshal(meta)
	c := container{
		suffix: "offsite", image: rcloneImage,
		entrypoint: []string{"sh", "-c"}, cmd: []string{shipperScript},
		env: append(d.offsite.env(), "REF="+ref, "META="+string(b)),
		mounts: []mount{
			{volume: vol(ref, "backups"), path: "/backups", readOnly: true},
			{volume: vol(ref, "db-config"), path: "/config", readOnly: true},
		},
	}
	return d.run(ctx, Spec{Ref: ref}, c, name, "flobase-"+ref)
}

// StopOffsite removes a project's sync container, for when off-site
// storage has been turned off. The copies already made stay in the bucket.
func (d *Docker) StopOffsite(ctx context.Context, ref string) error {
	err := d.call(ctx, "DELETE", "/containers/flobase-"+ref+"-offsite?force=true", nil, nil)
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

// OffsiteEnabled reports whether copies leave this machine.
func (d *Docker) OffsiteEnabled() bool { return d.offsite != nil }

// rclone runs a script in a throwaway rclone container and returns its
// output.
func (d *Docker) rclone(ctx context.Context, ref, script string, env []string, mounts []mount) (string, error) {
	if d.offsite == nil {
		return "", fmt.Errorf("off-site storage is not set up")
	}
	if err := d.pull(ctx, rcloneImage); err != nil {
		return "", err
	}
	name := "flobase-rclone-" + keys.Ref()
	c := container{
		suffix: "rclone", image: rcloneImage, oneShot: true,
		entrypoint: []string{"sh", "-c"}, cmd: []string{rclonePrelude + script},
		env: append(d.offsite.env(), env...), mounts: mounts,
	}
	if err := d.run(ctx, Spec{Ref: ref}, c, name, "bridge"); err != nil {
		return "", err
	}
	return d.waitExit(ctx, name)
}

// OffsiteProjects lists every project with copies in off-site storage,
// including ones this FloBase no longer knows.
func (d *Docker) OffsiteProjects(ctx context.Context) ([]OffsiteProject, error) {
	out, err := d.rclone(ctx, "platform", `for r in $(rclone lsf "$ROOT" --dirs-only 2>/dev/null); do
  r=${r%/}
  echo "M $r $(rclone cat "$ROOT$r/meta.json" 2>/dev/null || echo '{}')"
  for b in $(rclone lsf "$ROOT$r/base" --dirs-only 2>/dev/null); do
    b=${b%/}
    f=$(rclone cat "$ROOT$r/base/$b/finished" 2>/dev/null) || continue
    echo "B $r $b $f"
  done
  echo "W $r $(rclone lsf "$ROOT$r/wal" --files-only --format t 2>/dev/null | sort | tail -n 1)"
done`, nil, nil)
	if err != nil {
		return nil, err
	}
	byRef := map[string]*OffsiteProject{}
	var order []string
	get := func(ref string) *OffsiteProject {
		if p, ok := byRef[ref]; ok {
			return p
		}
		p := &OffsiteProject{Ref: ref, Meta: map[string]any{}, Backups: []Backup{}}
		byRef[ref] = p
		order = append(order, ref)
		return p
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimSpace(line), " ", 3)
		if len(f) < 2 {
			continue
		}
		p := get(f[1])
		switch {
		case f[0] == "M" && len(f) == 3:
			_ = json.Unmarshal([]byte(f[2]), &p.Meta)
		case f[0] == "B" && len(f) == 3:
			parts := strings.Fields(f[2])
			if len(parts) == 2 {
				if t, err := time.Parse(time.RFC3339, parts[1]); err == nil {
					p.Backups = append(p.Backups, Backup{Name: parts[0], Finished: t})
				}
			}
		case f[0] == "W" && len(f) == 3:
			if t, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(f[2])); err == nil {
				p.LastChange = &t
			}
		}
	}
	list := make([]OffsiteProject, 0, len(order))
	for _, ref := range order {
		p := byRef[ref]
		if len(p.Backups) > 0 {
			list = append(list, *p)
		}
	}
	return list, nil
}

// offsitePrune deletes off-site copies of pruned backups and changes.
func (d *Docker) offsitePrune(ctx context.Context, ref string, backups, walFiles []string) error {
	script := `D="${ROOT}$REF"
for b in $BACKUPS; do rclone purge "$D/base/$b" || true; done
if [ -n "$WAL" ]; then printf '%s\n' $WAL > /tmp/list; rclone delete "$D/wal" --files-from-raw /tmp/list; fi`
	_, err := d.rclone(ctx, ref, script, []string{
		"REF=" + ref, "BACKUPS=" + strings.Join(backups, " "), "WAL=" + strings.Join(walFiles, " "),
	}, nil)
	return err
}

// fetchContainer downloads what a restore needs from off-site storage into
// the new project's incoming volume, laid out as /restore expects.
func (d *Docker) fetchContainer(s Spec) container {
	walStep := `rclone copy "$D/wal" /restore/wal -q`
	if s.Restore.Immediate {
		walStep = "mkdir -p /restore/wal" // the backup carries the changes it needs
	}
	return container{
		suffix: "fetch", image: rcloneImage, oneShot: true,
		entrypoint: []string{"sh", "-c"},
		cmd: []string{rclonePrelude + `D="${ROOT}$FROM"
rclone copy "$D/base/$BACKUP" "/restore/base/$BACKUP" -q
` + walStep + `
rclone copy "$D/config" /restore/config -q
test -f "/restore/base/$BACKUP/base.tar.gz"
echo "fetched $BACKUP"`},
		env:    append(d.offsite.env(), "FROM="+s.Restore.FromRef, "BACKUP="+safeName(s.Restore.Backup)),
		mounts: []mount{{volume: vol(s.Ref, "incoming"), path: "/restore"}},
	}
}
