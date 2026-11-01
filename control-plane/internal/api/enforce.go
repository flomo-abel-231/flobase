package api

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"

	"flobase/control-plane/internal/catalog"
	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

// In the cloud, a plan's database size is a limit. Its workspace's owners
// and admins are told when a database reaches 80% and 90% of it; at 100%
// the database turns read-only, so apps can still read but not write,
// until data is deleted or the project moves to a bigger plan. FloBase's
// SQL editor and the table editor's delete still work on a read-only
// database, so people can make room. Self-hosted FloBase measures sizes
// but enforces nothing.

const (
	sizeOK       = ""
	sizeWarn80   = "warn80"
	sizeWarn90   = "warn90"
	sizeReadOnly = "readonly"
)

// sizeState is where a database of used bytes stands against a plan of
// limit bytes, given where it stood. Once read-only it stays so until it
// is back under 95%, so it does not flip with every small change.
func sizeState(used, limit int64, current string) string {
	if limit <= 0 {
		return sizeOK
	}
	share := float64(used) / float64(limit)
	switch {
	case share >= 1, current == sizeReadOnly && share >= 0.95:
		return sizeReadOnly
	case share >= 0.9:
		return sizeWarn90
	case share >= 0.8:
		return sizeWarn80
	}
	return sizeOK
}

func planBytes(plan string) int64 {
	p, _ := catalog.PlanByID(plan)
	return int64(planMB(p)) * 1_000_000
}

// specFor is what the provisioner needs to act on a project.
func (s *Server) specFor(p store.Project, plan catalog.Plan) provisioner.Spec {
	return provisioner.Spec{
		Ref: p.Ref, Engine: p.Engine, Plan: plan, User: p.DBUser, Password: p.DBPassword, Database: p.DBName,
		JWTSecret: p.JWTSecret, AuthURL: s.apiURL(p.Ref) + "/auth/v1",
	}
}

// enforceSize moves a project to the state its size calls for, switching
// writes off or on and telling the workspace when that changes.
func (s *Server) enforceSize(ctx context.Context, p store.Project) {
	if p.SizeBytes == nil || p.Status != store.StatusActive {
		return
	}
	next := sizeOK
	if s.billing() {
		next = sizeState(*p.SizeBytes, planBytes(p.Plan), p.SizeState)
	}
	if next == p.SizeState {
		return
	}
	plan, _ := catalog.PlanByID(p.Plan)
	if (next == sizeReadOnly) != (p.SizeState == sizeReadOnly) {
		if err := s.prov.SetReadOnly(ctx, s.specFor(p, plan), next == sizeReadOnly); err != nil {
			s.log.Warn("size enforcement", "ref", p.Ref, "read_only", next == sizeReadOnly, "err", err)
			return // try again on the next check
		}
		s.log.Info("size enforcement", "ref", p.Ref, "read_only", next == sizeReadOnly, "bytes", *p.SizeBytes)
	}
	if err := s.store.SetSizeState(ctx, p.Ref, next); err != nil {
		s.log.Warn("size enforcement", "ref", p.Ref, "err", err)
		return
	}
	// Tell people when it gets worse, and when writes come back.
	if rank(next) > rank(p.SizeState) || p.SizeState == sizeReadOnly {
		if err := s.notifySize(ctx, p, next); err != nil && err != errNoMail {
			s.log.Warn("size email", "ref", p.Ref, "err", err)
		}
	}
}

func rank(state string) int {
	return map[string]int{sizeOK: 0, sizeWarn80: 1, sizeWarn90: 2, sizeReadOnly: 3}[state]
}

func (s *Server) notifySize(ctx context.Context, p store.Project, state string) error {
	used := formatMB(int(*p.SizeBytes / 1_000_000))
	limit := formatMB(int(planBytes(p.Plan) / 1_000_000))
	var subject, body string
	switch state {
	case sizeWarn80, sizeWarn90:
		pct := map[string]string{sizeWarn80: "80%", sizeWarn90: "90%"}[state]
		subject = fmt.Sprintf("FloBase: %s has used %s of its storage", p.Name, pct)
		body = fmt.Sprintf("Your project %q (%s) holds %s of the %s its plan allows.\n\nWhen it reaches the limit the database turns read-only: apps can still read, but writes are refused until you delete data or move the project to a bigger plan.\n", p.Name, p.Ref, used, limit)
	case sizeReadOnly:
		subject = fmt.Sprintf("FloBase: %s is read-only: storage full", p.Name)
		body = fmt.Sprintf("Your project %q (%s) holds %s, more than the %s its plan allows, so its database is read-only now. Apps can read but not write.\n\nTo make room, delete data in the dashboard's SQL editor or table editor (they still work), then run VACUUM FULL on big tables so the space is freed; or move the project to a bigger plan on its Configuration > Plan page. Writes come back within 10 minutes of the database fitting again.\n", p.Name, p.Ref, used, limit)
	default:
		subject = fmt.Sprintf("FloBase: %s can write again", p.Name)
		body = fmt.Sprintf("Your project %q (%s) fits its plan again (%s of %s), so its database takes writes again.\n", p.Name, p.Ref, used, limit)
	}
	return s.notifyWorkspace(ctx, p, subject, body)
}

// notifyWorkspace emails a project's workspace owners and admins.
func (s *Server) notifyWorkspace(ctx context.Context, p store.Project, subject, body string) error {
	if !s.cfg.Mail.enabled() {
		return errNoMail
	}
	members, err := s.store.Members(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	var to []string
	for _, m := range members {
		if m.WorkspaceRole == "owner" || m.WorkspaceRole == "admin" {
			to = append(to, m.User.Email)
		}
	}
	if s.cfg.DashboardURL != "" {
		body += fmt.Sprintf("\nOpen the project: %s/#/project/%s\n", strings.TrimRight(s.cfg.DashboardURL, "/"), p.Ref)
	}
	body += "\n— FloBase\n"
	return s.cfg.Mail.send(to, subject, body)
}

// writable lets this session write to a read-only Postgres database. It
// answers false after writing the error.
func (s *Server) writable(ctx context.Context, conn *pgx.Conn, p store.Project) bool {
	if p.SizeState != sizeReadOnly {
		return true
	}
	_, err := conn.Exec(ctx, "set default_transaction_read_only = off")
	return err == nil
}

// liftMySQL makes a read-only MySQL schema writable while the SQL editor
// runs a query, and returns what puts it back.
func (s *Server) liftMySQL(ctx context.Context, p store.Project) (func(), error) {
	host, port := s.prov.Addr(p.Ref, "db", 3306, p.DBPort)
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd = "root", p.DBPassword
	cfg.Net, cfg.Addr = "tcp", host+":"+strconv.Itoa(port)
	cfg.Timeout = 5 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	if err := provisioner.MySQLReadOnly(ctx, db, p.DBName, false); err != nil {
		db.Close()
		return nil, err
	}
	return func() {
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := provisioner.MySQLReadOnly(ctx, db, p.DBName, true); err != nil {
			s.log.Warn("restore read-only", "ref", p.Ref, "err", err)
		}
	}, nil
}
