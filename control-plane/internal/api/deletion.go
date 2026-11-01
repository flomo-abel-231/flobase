package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"flobase/control-plane/internal/store"
)

// Deleting a project is guarded twice: the dashboard asks for the
// project's name (and deleteProject checks it), and when the project holds
// data it offers a backup to cloud storage first. Off-site copies outlive
// the project, so it can be restored later from Off-site backups.

func (s *Server) deletionRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/projects/{ref}/delete-check", s.auth(s.deleteCheck))
	mux.Handle("POST /api/v1/projects/{ref}/backups/offsite", s.auth(s.backupToCloud))
}

type deleteCheck struct {
	// HasData is true when the project holds tables or users, or when its
	// engine cannot be inspected.
	HasData bool   `json:"has_data"`
	Summary string `json:"summary"`
	// CloudBackup is true when a backup to cloud storage can be taken now:
	// a Postgres project with an active storage location.
	CloudBackup  bool   `json:"cloud_backup"`
	Bucket       string `json:"bucket,omitempty"`
	EngineBackup bool   `json:"engine_backup"`
}

// Supabase's own schemas; tables there are not the user's data.
const systemSchemas = `('pg_catalog','information_schema','auth','storage','graphql','graphql_public','realtime',
	'_realtime','extensions','pgbouncer','vault','supabase_functions','supabase_migrations','net','cron','pgsodium',
	'pgsodium_masks','pg_toast')`

func (s *Server) deleteCheck(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Project(r.Context(), r.PathValue("ref"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Project not found.")
		return
	}
	bucket, _, enabled := s.prov.OffsiteInfo()
	c := deleteCheck{EngineBackup: p.Engine == "postgres", Bucket: bucket}
	c.CloudBackup = c.EngineBackup && enabled && p.Status == store.StatusActive
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	switch {
	case p.Status != store.StatusActive:
		c.Summary = "The project is not running, so its data cannot be checked."
	case p.Engine == "postgres":
		c.HasData, c.Summary = s.postgresData(ctx, p)
	case p.Engine == "mysql":
		c.HasData, c.Summary = s.mysqlData(ctx, p)
	default:
		c.HasData, c.Summary = true, "MongoDB-compatible projects cannot be checked here; assume they hold data."
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) postgresData(ctx context.Context, p store.Project) (bool, string) {
	conn, err := s.pgConnect(ctx, p)
	if err != nil {
		return true, "The database could not be reached to check its data."
	}
	defer conn.Close(context.Background())
	var tables, rows, users int64
	err = conn.QueryRow(ctx, `
		select
			(select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace
				where c.relkind in ('r', 'p') and n.nspname not in `+systemSchemas+` and n.nspname not like 'pg_temp%'),
			(select coalesce(sum(n_live_tup), 0) from pg_stat_user_tables where schemaname not in `+systemSchemas+`),
			(select count(*) from auth.users)`).Scan(&tables, &rows, &users)
	if err != nil {
		return true, "The database's data could not be counted."
	}
	return tables > 0 || users > 0, summary(tables, rows, users)
}

func (s *Server) mysqlData(ctx context.Context, p store.Project) (bool, string) {
	db, err := s.mysqlOpen(p)
	if err != nil {
		return true, "The database could not be reached to check its data."
	}
	defer db.Close()
	var tables, rows int64
	err = db.QueryRowContext(ctx, `select count(*), coalesce(sum(table_rows), 0) from information_schema.tables
		where table_schema = database() and table_type = 'BASE TABLE'`).Scan(&tables, &rows)
	if err != nil {
		return true, "The database's data could not be counted."
	}
	return tables > 0, summary(tables, rows, -1)
}

func summary(tables, rows, users int64) string {
	plural := func(n int64, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.FormatInt(n, 10) + " " + many
	}
	// Row counts are Postgres's estimates, which read 0 until statistics
	// catch up (right after a restore), so they are shown only when known.
	out := plural(tables, "table", "tables")
	if rows > 0 {
		out += " with about " + plural(rows, "row", "rows")
	}
	if users >= 0 {
		out += " and " + plural(users, "user", "users")
	}
	return out
}

// backupToCloud takes a full backup and copies it, with every archived
// change, to the active storage location now.
func (s *Server) backupToCloud(w http.ResponseWriter, r *http.Request) {
	p, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	bucket, _, enabled := s.prov.OffsiteInfo()
	if !enabled {
		writeError(w, http.StatusConflict, "Add a storage location under Off-site backups first.")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Minute)
	defer cancel()
	s.ensureOffsite(ctx, p)
	b, err := s.backup(p.Ref, "cloud")
	if err != nil {
		writeError(w, http.StatusConflict, "Backup failed: "+err.Error())
		return
	}
	if err := s.prov.FlushArchive(ctx, p.Ref); err != nil {
		s.log.Warn("flush archive before cloud backup", "ref", p.Ref, "err", err)
	}
	if err := s.prov.OffsiteSyncNow(ctx, p.Ref); err != nil {
		writeError(w, http.StatusBadGateway, "The backup was taken but could not be copied to "+bucket+": "+err.Error())
		return
	}
	s.log.Info("cloud backup", "ref", p.Ref, "bucket", bucket, "backup", b.Name)
	writeJSON(w, http.StatusOK, map[string]any{"bucket": bucket, "backup": b.Name, "finished": b.Finished})
}
