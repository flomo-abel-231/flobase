package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"flobase/control-plane/internal/catalog"
	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

// Storage capacity: each plan comes with a database size, and the
// platform's owner can cap how much storage a workspace's projects may
// take in all, the way a virtual machine is given a disk. A project fits
// when the sizes of the workspace's plans, its own included, stay within
// the cap. FloBase measures every project's database each hour so people
// can see what they use.

func (s *Server) capacityRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/storage", s.auth(s.workspaceStorage))
	mux.Handle("GET /api/v1/admin/workspaces", s.platformAdmin(s.adminWorkspaces))
	mux.Handle("PUT /api/v1/admin/workspaces/{id}/storage", s.platformAdmin(s.setWorkspaceStorage))
	mux.Handle("PUT /api/v1/projects/{ref}/plan", s.auth(s.changePlan))
}

// planMB is the database size a plan comes with.
func planMB(p catalog.Plan) int { return int(math.Round(p.DiskGB * 1000)) }

func projectMB(p store.Project) int {
	plan, _ := catalog.PlanByID(p.Plan)
	return planMB(plan)
}

type storageProject struct {
	Ref       string     `json:"ref"`
	Name      string     `json:"name"`
	Engine    string     `json:"engine"`
	Plan      string     `json:"plan"`
	Status    string     `json:"status"`
	PlanMB    int        `json:"plan_mb"`
	UsedBytes *int64     `json:"used_bytes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type capacityView struct {
	LimitMB     *int             `json:"limit_mb"` // nil = no limit
	AllocatedMB int              `json:"allocated_mb"`
	UsedBytes   int64            `json:"used_bytes"`
	Projects    []storageProject `json:"projects"`
}

func (s *Server) storageOf(ctx context.Context, ws string) (capacityView, error) {
	v := capacityView{Projects: []storageProject{}}
	limit, err := s.store.StorageLimit(ctx, ws)
	if err != nil {
		return v, err
	}
	v.LimitMB = limit
	projects, err := s.store.WorkspaceProjects(ctx, ws)
	if err != nil {
		return v, err
	}
	for _, p := range projects {
		if p.Status == store.StatusDeleting {
			continue
		}
		sp := storageProject{Ref: p.Ref, Name: p.Name, Engine: p.Engine, Plan: p.Plan, Status: p.Status,
			PlanMB: projectMB(p), UsedBytes: p.SizeBytes}
		if s.billing() {
			sp.ExpiresAt = p.ExpiresAt
		}
		v.AllocatedMB += sp.PlanMB
		if p.SizeBytes != nil {
			v.UsedBytes += *p.SizeBytes
		}
		v.Projects = append(v.Projects, sp)
	}
	return v, nil
}

// fits says whether a workspace can take a project of mb, leaving out the
// project ref (one changing plan). The message says why not.
func (s *Server) fits(ctx context.Context, ws string, mb int, ref string) (bool, string, error) {
	v, err := s.storageOf(ctx, ws)
	if err != nil || v.LimitMB == nil {
		return true, "", err
	}
	used := v.AllocatedMB
	for _, p := range v.Projects {
		if p.Ref == ref {
			used -= p.PlanMB
		}
	}
	if used+mb <= *v.LimitMB {
		return true, "", nil
	}
	return false, fmt.Sprintf("This workspace has %s of storage and its projects take %s; this needs %s more. Ask the platform's owner for more storage, or pick a smaller plan.",
		formatMB(*v.LimitMB), formatMB(used), formatMB(used+mb-*v.LimitMB)), nil
}

func formatMB(mb int) string {
	if mb >= 1000 && mb%100 == 0 {
		return fmt.Sprintf("%g GB", float64(mb)/1000)
	}
	return fmt.Sprintf("%d MB", mb)
}

func (s *Server) workspaceStorage(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	v, err := s.storageOf(r.Context(), ws)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Workspace not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type adminWorkspace struct {
	store.WorkspaceStorage
	Projects    int   `json:"projects"`
	AllocatedMB int   `json:"allocated_mb"`
	UsedBytes   int64 `json:"used_bytes"`
}

func (s *Server) adminWorkspaces(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.WorkspacesStorage(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	projects, err := s.store.Projects(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	out := make([]adminWorkspace, 0, len(list))
	for _, ws := range list {
		a := adminWorkspace{WorkspaceStorage: ws}
		for _, p := range projects {
			if p.WorkspaceID != ws.ID || p.Status == store.StatusDeleting {
				continue
			}
			a.Projects++
			a.AllocatedMB += projectMB(p)
			if p.SizeBytes != nil {
				a.UsedBytes += *p.SizeBytes
			}
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setWorkspaceStorage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LimitMB *int `json:"limit_mb"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.LimitMB != nil && (*req.LimitMB < 0 || *req.LimitMB > 100_000_000) {
		writeError(w, http.StatusBadRequest, "The limit is a number of MB from 0 up, or empty for no limit.")
		return
	}
	if err := s.store.SetStorageLimit(r.Context(), r.PathValue("id"), req.LimitMB); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Workspace not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	s.log.Info("workspace storage limit set", "workspace", r.PathValue("id"), "limit_mb", req.LimitMB)
	s.adminWorkspaces(w, r)
}

// changePlan moves a project to another plan: its database restarts with
// the new plan's CPU and memory, keeping its data.
func (s *Server) changePlan(w http.ResponseWriter, r *http.Request) {
	if !who(r).canManageTeam() {
		writeError(w, http.StatusForbidden, "Only the workspace's owner and admins can change a project's plan.")
		return
	}
	p, ok := s.activeProject(w, r)
	if !ok {
		return
	}
	var req struct {
		Plan string `json:"plan"`
	}
	if !decode(w, r, &req) {
		return
	}
	plan, ok := catalog.PlanByID(req.Plan)
	if !ok {
		writeError(w, http.StatusBadRequest, "Unknown plan.")
		return
	}
	if plan.ID == p.Plan {
		writeJSON(w, http.StatusOK, s.view(p, false))
		return
	}
	if !s.planOpen(r, plan) {
		writeError(w, http.StatusPaymentRequired, plan.Name+" opens once payments are set up.")
		return
	}
	if ok, msg, err := s.fits(r.Context(), p.WorkspaceID, planMB(plan), p.Ref); err != nil {
		s.internal(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusConflict, msg)
		return
	}
	if p.SizeBytes != nil && *p.SizeBytes > int64(planMB(plan))*1_000_000 {
		writeError(w, http.StatusConflict, fmt.Sprintf("The database holds %s, more than %s allows (%s). Delete data first or pick a bigger plan.",
			formatMB(int(*p.SizeBytes/1_000_000)), plan.Name, formatMB(planMB(plan))))
		return
	}
	// A backup reads from the database container, which restarts here.
	if !s.jobs.start(p.Ref) {
		writeError(w, http.StatusConflict, "A backup of this project is running. Try again when it finishes.")
		return
	}
	defer s.jobs.done(p.Ref)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()

	// Free cloud projects are deleted a month after moving to Free; on any
	// other plan they are kept.
	var expires *time.Time
	if s.billing() && plan.Price == 0 {
		t := time.Now().Add(freeDays * 24 * time.Hour)
		expires = &t
	}
	old, oldExpires := p.Plan, p.ExpiresAt
	if err := s.store.SetPlan(ctx, p.Ref, plan.ID, expires); err != nil {
		s.internal(w, err)
		return
	}
	spec := provisioner.Spec{
		Ref: p.Ref, Engine: p.Engine, Plan: plan, User: p.DBUser, Password: p.DBPassword, Database: p.DBName,
		JWTSecret: p.JWTSecret, AuthURL: s.apiURL(p.Ref) + "/auth/v1",
	}
	if err := s.prov.Recreate(ctx, spec); err != nil {
		s.log.Error("change plan", "ref", p.Ref, "err", err)
		_ = s.store.SetPlan(context.Background(), p.Ref, old, oldExpires)
		writeError(w, http.StatusBadGateway, "Could not restart the project on the new plan: "+err.Error())
		return
	}
	s.log.Info("plan changed", "ref", p.Ref, "from", old, "to", plan.ID)
	p.Plan, p.ExpiresAt = plan.ID, expires
	writeJSON(w, http.StatusOK, s.view(p, false))
}

// StartUsage measures every running project's database each hour.
func (s *Server) StartUsage(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			s.measure(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func (s *Server) measure(ctx context.Context) {
	projects, err := s.store.Projects(ctx)
	if err != nil {
		s.log.Warn("measure storage", "err", err)
		return
	}
	for _, p := range projects {
		if p.Status != store.StatusActive {
			continue
		}
		mctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		n, err := s.databaseSize(mctx, p)
		cancel()
		if err != nil {
			s.log.Debug("measure storage", "ref", p.Ref, "err", err)
			continue
		}
		if err := s.store.SetSize(ctx, p.Ref, n); err != nil {
			s.log.Warn("measure storage", "ref", p.Ref, "err", err)
		}
	}
}

var errUnmeasured = errors.New("this engine's size is not measured")

func (s *Server) databaseSize(ctx context.Context, p store.Project) (int64, error) {
	var n int64
	switch p.Engine {
	case "postgres":
		conn, err := s.pgConnect(ctx, p)
		if err != nil {
			return 0, err
		}
		defer conn.Close(context.Background())
		err = conn.QueryRow(ctx, `select pg_database_size(current_database())`).Scan(&n)
		return n, err
	case "mysql":
		db, err := s.mysqlOpen(p)
		if err != nil {
			return 0, err
		}
		defer db.Close()
		err = db.QueryRowContext(ctx, `select coalesce(sum(data_length + index_length), 0) from information_schema.tables
			where table_schema = database()`).Scan(&n)
		return n, err
	}
	return 0, errUnmeasured
}
