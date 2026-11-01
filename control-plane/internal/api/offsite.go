package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

// Off-site copies of Postgres backups, in S3-compatible storage.

// projectMeta is stored beside a project's off-site copies, so a FloBase
// that never knew the project can still name and size it on restore.
func projectMeta(p store.Project) map[string]any {
	return map[string]any{
		"ref": p.Ref, "name": p.Name, "engine": p.Engine, "plan": p.Plan,
		"created_at": p.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// ensureOffsite starts a project's sync container, logging failures: an
// off-site problem must never block the project itself.
func (s *Server) ensureOffsite(ctx context.Context, p store.Project) {
	if p.Engine != "postgres" || !s.prov.OffsiteEnabled() {
		return
	}
	if err := s.prov.EnsureOffsite(ctx, p.Ref, projectMeta(p)); err != nil {
		s.log.Error("start off-site sync", "ref", p.Ref, "err", err)
	}
}

// EnsureOffsiteAll matches sync containers to the configuration: started
// for projects made before off-site storage was set up, removed when it
// has been turned off.
func (s *Server) EnsureOffsiteAll(ctx context.Context) {
	projects, err := s.store.Projects(ctx)
	if err != nil {
		return
	}
	for _, p := range projects {
		if !s.prov.OffsiteEnabled() {
			_ = s.prov.StopOffsite(ctx, p.Ref)
			continue
		}
		if p.Status == store.StatusActive {
			if _, err := s.prov.Backups(ctx, p.Ref); err == nil {
				s.ensureOffsite(ctx, p)
			}
		}
	}
}

// offsiteStatus reads the sync container's last report.
func (s *Server) offsiteStatus(ctx context.Context, ref string) *offsiteStatus {
	bucket, encrypted, enabled := s.prov.OffsiteInfo()
	st := &offsiteStatus{Enabled: enabled, Bucket: bucket, Encrypted: encrypted}
	if !st.Enabled {
		return st
	}
	text, err := s.prov.Logs(ctx, ref, "offsite", 50)
	if err != nil {
		st.Failing = true
		return st
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		// Lines are "<docker timestamp> synced <time>" or "... sync failed <time>".
		f := strings.Fields(lines[i])
		if len(f) >= 3 && f[1] == "synced" {
			if t, err := time.Parse(time.RFC3339, f[2]); err == nil {
				st.LastSync = &t
			}
			break
		}
		if len(f) >= 3 && f[1] == "sync" {
			st.Failing = true
			break
		}
	}
	return st
}

type offsiteProjectView struct {
	provisioner.OffsiteProject
	// Here is true when this FloBase still runs the project.
	Here bool `json:"here"`
}

func (s *Server) listOffsite(w http.ResponseWriter, r *http.Request) {
	if !s.prov.OffsiteEnabled() {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "projects": []any{}})
		return
	}
	list, err := s.prov.OffsiteProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Could not read off-site storage: "+err.Error())
		return
	}
	bucket, encrypted, _ := s.prov.OffsiteInfo()
	out := make([]offsiteProjectView, 0, len(list))
	for _, p := range list {
		_, err := s.store.Project(r.Context(), p.Ref)
		out = append(out, offsiteProjectView{OffsiteProject: p, Here: err == nil})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true, "bucket": bucket, "encrypted": encrypted, "projects": out,
	})
}

func (s *Server) restoreOffsite(w http.ResponseWriter, r *http.Request) {
	if !s.prov.OffsiteEnabled() {
		writeError(w, http.StatusConflict, "Off-site storage is not set up.")
		return
	}
	var req restoreRequest
	if !decode(w, r, &req) {
		return
	}
	list, err := s.prov.OffsiteProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Could not read off-site storage: "+err.Error())
		return
	}
	var src *provisioner.OffsiteProject
	for i := range list {
		if list[i].Ref == r.PathValue("ref") {
			src = &list[i]
		}
	}
	if src == nil || len(src.Backups) == 0 {
		writeError(w, http.StatusNotFound, "No off-site backups for that project.")
		return
	}
	spec := provisioner.Restore{FromRef: src.Ref, Offsite: true}
	switch {
	case req.Backup != "":
		for _, b := range src.Backups {
			if b.Name == req.Backup {
				spec.Backup, spec.Immediate = b.Name, true
			}
		}
		if spec.Backup == "" {
			writeError(w, http.StatusNotFound, "Backup not found.")
			return
		}
	case req.Latest || req.Time != nil:
		target := time.Now().UTC()
		if req.Time != nil {
			target = req.Time.UTC()
			spec.Target = target
		}
		if target.Before(src.Backups[0].Finished) {
			writeError(w, http.StatusBadRequest, "Pick a time after the oldest off-site backup.")
			return
		}
		if req.Time != nil && src.LastChange != nil && target.After(src.LastChange.Add(time.Minute)) {
			writeError(w, http.StatusBadRequest, "Off-site copies only reach "+src.LastChange.Format(time.RFC3339)+".")
			return
		}
		for _, b := range src.Backups {
			if !b.Finished.After(target) {
				spec.Backup = b.Name
			}
		}
	default:
		writeError(w, http.StatusBadRequest, "Choose latest, a time, or a backup.")
		return
	}
	name, _ := src.Meta["name"].(string)
	if name == "" {
		name = src.Ref
	}
	plan, _ := src.Meta["plan"].(string)
	s.startRestore(w, r, spec, req.Name, name, plan, "local")
}
