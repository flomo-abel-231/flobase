package provisioner

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Backups live in the project's backups volume, mounted at /backups in its
// database container: base/<name>/ holds pg_basebackup tarballs and a
// "finished" timestamp, wal/ the archived change log.

func dbContainer(ref string) string { return "flobase-" + ref + "-db" }

func (d *Docker) TakeBackup(ctx context.Context, ref string) (Backup, error) {
	// The backup is labelled with its name so its history file in wal/ can
	// be found again when older backups are pruned.
	out, err := d.exec(ctx, dbContainer(ref), "postgres", "sh", "-c", `set -e
n=$(date -u +%Y%m%dT%H%M%SZ)
d=/backups/base/$n
rm -rf "$d.part"
pg_basebackup -h /var/run/postgresql -U supabase_admin -D "$d.part" -Ft -z -X stream -c fast -l "$n"
mv "$d.part" "$d"
date -u +%Y-%m-%dT%H:%M:%SZ > "$d/finished"
echo "$n"`)
	if err != nil {
		return Backup{}, err
	}
	name := lastLine(out)
	list, err := d.Backups(ctx, ref)
	if err != nil {
		return Backup{}, err
	}
	for _, b := range list {
		if b.Name == name {
			return b, nil
		}
	}
	return Backup{Name: name, Finished: time.Now().UTC()}, nil
}

func (d *Docker) Backups(ctx context.Context, ref string) ([]Backup, error) {
	out, err := d.exec(ctx, dbContainer(ref), "postgres", "sh", "-c", `for d in /backups/base/*/; do
  [ -f "$d/finished" ] || continue
  printf '%s %s %s\n' "$(basename "$d")" "$(cat "$d/finished")" "$(du -sk "$d" | cut -f1)"
done`)
	if err != nil {
		return nil, err
	}
	var list []Backup
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		finished, err := time.Parse(time.RFC3339, f[1])
		if err != nil {
			continue
		}
		kb, _ := strconv.ParseInt(f[2], 10, 64)
		list = append(list, Backup{Name: f[0], Finished: finished, SizeBytes: kb * 1024})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Finished.Before(list[j].Finished) })
	return list, nil
}

func (d *Docker) DeleteBackups(ctx context.Context, ref string, names []string, oldestKept string) error {
	script := "set -e\n"
	for _, n := range names {
		script += "rm -rf '/backups/base/" + safeName(n) + "'\n"
	}
	// pg_archivecleanup drops every archived change older than the oldest
	// remaining backup's start, found through its history file's label; -n
	// first lists those files so the off-site copies go too.
	script += `f=$(grep -l "LABEL: ` + safeName(oldestKept) + `" /backups/wal/*.backup 2>/dev/null | head -n 1)
if [ -n "$f" ]; then
  pg_archivecleanup -n /backups/wal "$(basename "$f")"
  pg_archivecleanup /backups/wal "$(basename "$f")"
fi`
	out, err := d.exec(ctx, dbContainer(ref), "postgres", "sh", "-c", script)
	if err != nil || d.currentOffsite() == nil {
		return err
	}
	var wal []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			wal = append(wal, safeWAL(line[strings.LastIndex(line, "/")+1:]))
		}
	}
	var safe []string
	for _, n := range names {
		safe = append(safe, safeName(n))
	}
	return d.offsitePrune(ctx, ref, safe, wal)
}

// safeWAL keeps a WAL file name to hex digits.
func safeWAL(n string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'F' {
			return r
		}
		return -1
	}, n)
}

// FlushArchive closes the change log file in use and waits until it is
// archived. pg_switch_wal returns one byte past the end of the file it
// closed (or, with nothing new since the last switch, the start of the
// current file), so the byte before it always lies in a completed file.
func (d *Docker) FlushArchive(ctx context.Context, ref string) error {
	_, err := d.exec(ctx, dbContainer(ref), "postgres", "sh", "-c", `set -e
w=$(psql -h /var/run/postgresql -U supabase_admin -d postgres -tAc "select pg_walfile_name(pg_switch_wal() - 1)")
i=0
until [ -f "/backups/wal/$w" ]; do i=$((i+1)); [ $i -lt 60 ] || exit 1; sleep 1; done`)
	return err
}

// safeName keeps a backup name to the characters TakeBackup produces, so it
// is safe inside a shell script.
func safeName(n string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' {
			return r
		}
		return -1
	}, n)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
