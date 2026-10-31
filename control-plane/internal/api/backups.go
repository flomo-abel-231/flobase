package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"

	"flobase/control-plane/internal/catalog"
	"flobase/control-plane/internal/keys"
	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

// Backups for Postgres projects: every change is archived continuously, a
// full backup is taken daily, and a restore always creates a new project
// so the original is never overwritten.

// retentionDays is how far back a plan can restore.
var retentionDays = map[string]int{"free": 1, "starter": 7, "pro": 7, "business": 14}

// policy says what a project's plan allows. Self-hosted has no billing, so
// every plan gets backups and point-in-time recovery there.
type policy struct {
	Enabled       bool `json:"enabled"`
	PointInTime   bool `json:"point_in_time"`
	RetentionDays int  `json:"retention_days"`
}

func (s *Server) policy(plan string) policy {
	if s.cfg.Mode != "cloud" {
		return policy{Enabled: true, PointInTime: true, RetentionDays: retentionDays[plan]}
	}
	return policy{
		Enabled:       plan != "free",
		PointInTime:   plan == "pro" || plan == "business",
		RetentionDays: retentionDays[plan],
	}
}

// backupJobs keeps one backup per project at a time and counts restores
// reading from each source, which must not be deleted meanwhile.
type backupJobs struct {
	mu      sync.Mutex
	running map[string]bool
	sources map[string]int
}

func (j *backupJobs) start(ref string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running[ref] {
		return false
	}
	j.running[ref] = true
	return true
}

func (j *backupJobs) done(ref string) {
	j.mu.Lock()
	delete(j.running, ref)
	j.mu.Unlock()
}

func (j *backupJobs) restoring(ref string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.sources[ref] > 0
}

func (j *backupJobs) addSource(ref string, n int) {
	j.mu.Lock()
	j.sources[ref] += n
	j.mu.Unlock()
}

func (s *Server) backupRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/projects/{ref}/backups", s.auth(s.listBackups))
	mux.Handle("POST /api/v1/projects/{ref}/backups", s.auth(s.backupNow))
	mux.Handle("POST /api/v1/projects/{ref}/restore", s.auth(s.restore))
	mux.Handle("GET /api/v1/offsite", s.auth(s.listOffsite))
	mux.Handle("POST /api/v1/offsite/{ref}/restore", s.auth(s.restoreOffsite))
}

// backup takes one backup and prunes what the plan no longer keeps.
func (s *Server) backup(ref, reason string) (provisioner.Backup, error) {
	if !s.jobs.start(ref) {
		return provisioner.Backup{}, errors.New("a backup of this project is already running")
	}
	defer s.jobs.done(ref)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	b, err := s.prov.TakeBackup(ctx, ref)
	if err != nil {
		s.log.Error("backup failed", "ref", ref, "reason", reason, "err", err)
		return b, err
	}
	s.log.Info("backup taken", "ref", ref, "reason", reason, "name", b.Name, "bytes", b.SizeBytes)
	if p, err := s.store.Project(ctx, ref); err == nil {
		s.prune(ctx, p)
	}
	return b, nil
}

// prune deletes backups older than the retention window, always keeping the
// newest so a restore stays possible.
func (s *Server) prune(ctx context.Context, p store.Project) {
	list, err := s.prov.Backups(ctx, p.Ref)
	if err != nil || len(list) < 2 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(s.policy(p.Plan).RetentionDays) * 24 * time.Hour)
	var drop []string
	keep := list[len(list)-1].Name
	for i, b := range list {
		if i < len(list)-1 && b.Finished.Before(cutoff) {
			drop = append(drop, b.Name)
		} else {
			keep = b.Name
			break
		}
	}
	if len(drop) == 0 {
		return
	}
	if err := s.prov.DeleteBackups(ctx, p.Ref, drop, keep); err != nil {
		s.log.Error("prune backups", "ref", p.Ref, "err", err)
	}
}

// RunBackups takes a daily backup of every active Postgres project whose
// newest backup is more than a day old, checking every 15 minutes.
func (s *Server) RunBackups(ctx context.Context) {
	tick := time.NewTicker(15 * time.Minute)
	defer tick.Stop()
	for {
		projects, err := s.store.Projects(ctx)
		if err == nil {
			for _, p := range projects {
				if p.Engine != "postgres" || p.Status != store.StatusActive || !s.policy(p.Plan).Enabled {
					continue
				}
				list, err := s.prov.Backups(ctx, p.Ref)
				if err != nil {
					continue // created before backups shipped, or not reachable
				}
				if len(list) == 0 || time.Since(list[len(list)-1].Finished) > 24*time.Hour {
					_, _ = s.backup(p.Ref, "daily")
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

type backupsView struct {
	policy
	Backups []provisioner.Backup `json:"backups"`
	// Window is the span a restore can target.
	Window *struct {
		From time.Time `json:"from"`
		To   time.Time `json:"to"`
	} `json:"window,omitempty"`
	Running bool           `json:"running"`
	Offsite *offsiteStatus `json:"offsite"`
}

type offsiteStatus struct {
	Enabled   bool       `json:"enabled"`
	Bucket    string     `json:"bucket,omitempty"`
	Encrypted bool       `json:"encrypted"`
	LastSync  *time.Time `json:"last_sync,omitempty"`
	Failing   bool       `json:"failing"`
}

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	p, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	v := backupsView{policy: s.policy(p.Plan), Backups: []provisioner.Backup{}}
	list, err := s.prov.Backups(r.Context(), p.Ref)
	if err != nil {
		writeError(w, http.StatusConflict, "This project was created before backups were available. Create a new project to get them.")
		return
	}
	if list != nil {
		v.Backups = list
	}
	if len(list) > 0 && v.PointInTime {
		v.Window = &struct {
			From time.Time `json:"from"`
			To   time.Time `json:"to"`
		}{From: list[0].Finished, To: time.Now().UTC()}
	}
	s.jobs.mu.Lock()
	v.Running = s.jobs.running[p.Ref]
	s.jobs.mu.Unlock()
	v.Offsite = s.offsiteStatus(r.Context(), p.Ref)
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) backupNow(w http.ResponseWriter, r *http.Request) {
	p, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	if !s.policy(p.Plan).Enabled {
		writeError(w, http.StatusForbidden, "Backups start on the Starter plan.")
		return
	}
	b, err := s.backup(p.Ref, "manual")
	if err != nil {
		writeError(w, http.StatusConflict, "Backup failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

type restoreRequest struct {
	// Exactly one of: Latest, Time (point in time), Backup (a full backup).
	Latest bool       `json:"latest"`
	Time   *time.Time `json:"time"`
	Backup string     `json:"backup"`
	Name   string     `json:"name"`
}

func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	src, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	var req restoreRequest
	if !decode(w, r, &req) {
		return
	}
	pol := s.policy(src.Plan)
	if !pol.Enabled {
		writeError(w, http.StatusForbidden, "Backups start on the Starter plan.")
		return
	}
	list, err := s.prov.Backups(r.Context(), src.Ref)
	if err != nil || len(list) == 0 {
		writeError(w, http.StatusConflict, "There is no backup to restore from yet.")
		return
	}
	spec := provisioner.Restore{FromRef: src.Ref}
	switch {
	case req.Backup != "":
		for _, b := range list {
			if b.Name == req.Backup {
				spec.Backup, spec.Immediate = b.Name, true
			}
		}
		if spec.Backup == "" {
			writeError(w, http.StatusNotFound, "Backup not found.")
			return
		}
	case req.Latest || req.Time != nil:
		if !pol.PointInTime {
			writeError(w, http.StatusForbidden, "Point-in-time recovery comes with the Pro plan; restore a daily backup instead.")
			return
		}
		target := time.Now().UTC()
		if req.Time != nil {
			target = req.Time.UTC()
			spec.Target = target
		}
		if target.Before(list[0].Finished) || target.After(time.Now().Add(time.Minute)) {
			writeError(w, http.StatusBadRequest, "Pick a time between the oldest backup and now.")
			return
		}
		// Replay starts from the newest backup finished before the target.
		for _, b := range list {
			if !b.Finished.After(target) {
				spec.Backup = b.Name
			}
		}
		// Archive the change log in progress so recent changes are included.
		if err := s.prov.FlushArchive(r.Context(), src.Ref); err != nil {
			s.log.Warn("flush archive", "ref", src.Ref, "err", err)
		}
	default:
		writeError(w, http.StatusBadRequest, "Choose latest, a time, or a backup.")
		return
	}

	s.startRestore(w, r, spec, req.Name, src.Name, src.Plan, src.Region)
}

// startRestore creates the new project record and provisions it from spec
// in the background.
func (s *Server) startRestore(w http.ResponseWriter, r *http.Request, spec provisioner.Restore, name, sourceName, planID, region string) {
	if name == "" {
		name = sourceName + " (restored)"
	}
	if utf8.RuneCountInString(name) > 64 {
		name = string([]rune(name)[:64])
	}
	plan, ok := catalog.PlanByID(planID)
	if !ok {
		plan, _ = catalog.PlanByID("starter")
	}
	ref, secret := keys.Ref(), keys.JWTSecret()
	p, err := s.store.CreateProject(r.Context(), store.Project{
		Ref: ref, Name: name, Engine: "postgres", Region: region, Plan: plan.ID,
		DBUser: "postgres", DBPassword: keys.Password(), DBName: "postgres", JWTSecret: secret,
		AnonKey: keys.APIKey(secret, ref, "anon"), ServiceKey: keys.APIKey(secret, ref, "service_role"),
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	if !spec.Offsite {
		s.jobs.addSource(spec.FromRef, 1)
	}
	go func() {
		if !spec.Offsite {
			defer s.jobs.addSource(spec.FromRef, -1)
		}
		s.provision(p, plan, &spec)
	}()
	writeJSON(w, http.StatusAccepted, s.view(p, true))
}
