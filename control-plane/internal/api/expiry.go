package api

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"flobase/control-plane/internal/store"
)

// In the cloud the Free plan is for trying FloBase: a free project is
// deleted a month after it is created, with no backup kept. Its
// workspace's owners and admins are warned 15, 10, 5, 3 and 1 days before,
// by email and with a banner in the dashboard, so they can back up their
// data first.

const freeDays = 30

var warnDays = []int{15, 10, 5, 3, 1}

// warning is the warning due for a project deleted in daysLeft days, given
// the last one sent: the smallest step daysLeft has reached, if not sent
// yet. 0 = none due.
func warning(daysLeft int, warned *int) int {
	due := 0
	for _, d := range warnDays {
		if daysLeft <= d {
			due = d
		}
	}
	if due == 0 || (warned != nil && *warned <= due) {
		return 0
	}
	return due
}

// daysLeft rounds up, so a project deleted in 14½ days is "15 days".
func daysLeft(until time.Time, now time.Time) int {
	return int(math.Ceil(until.Sub(now).Hours() / 24))
}

// StartExpiry checks free projects every hour, in the cloud only.
func (s *Server) StartExpiry(ctx context.Context) {
	if !s.billing() {
		return
	}
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			s.checkExpiry(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func (s *Server) checkExpiry(ctx context.Context) {
	if err := s.store.ExpireFreeProjects(ctx, warnDays[0]); err != nil {
		s.log.Warn("free projects", "err", err)
		return
	}
	projects, err := s.store.Projects(ctx)
	if err != nil {
		s.log.Warn("free projects", "err", err)
		return
	}
	now := time.Now()
	for _, p := range projects {
		if p.ExpiresAt == nil || p.Status == store.StatusDeleting || p.Status == store.StatusProvisioning {
			continue
		}
		left := daysLeft(*p.ExpiresAt, now)
		if left <= 0 {
			if s.jobs.restoring(p.Ref) {
				continue
			}
			s.log.Info("deleting expired free project", "ref", p.Ref)
			if err := s.store.SetStatus(ctx, p.Ref, store.StatusDeleting, "Free plan: deleted after 30 days."); err != nil {
				s.log.Warn("delete expired project", "ref", p.Ref, "err", err)
				continue
			}
			go s.Teardown(p.Ref)
			continue
		}
		due := warning(left, p.WarnedDays)
		if due == 0 {
			continue
		}
		if err := s.warnExpiry(ctx, p, left); err != nil {
			s.log.Warn("deletion warning email", "ref", p.Ref, "err", err)
			if err != errNoMail {
				continue // try again next hour
			}
		}
		if err := s.store.SetWarned(ctx, p.Ref, due); err != nil {
			s.log.Warn("deletion warning", "ref", p.Ref, "err", err)
		}
	}
}

// warnExpiry emails a project's workspace owners and admins.
func (s *Server) warnExpiry(ctx context.Context, p store.Project, left int) error {
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
	days := "days"
	if left == 1 {
		days = "day"
	}
	subject := fmt.Sprintf("FloBase: %s will be deleted in %d %s", p.Name, left, days)
	var b strings.Builder
	fmt.Fprintf(&b, "Your free FloBase project %q (%s) will be deleted on %s, in %d %s.\n\n",
		p.Name, p.Ref, p.ExpiresAt.UTC().Format("Monday 2 January 2006 at 15:04 UTC"), left, days)
	fmt.Fprintf(&b, "Free projects are deleted %d days after they are created, together with their data. FloBase does not keep a copy.\n\n", freeDays)
	b.WriteString("Back up your data before then. Free projects have no FloBase backups, so export it yourself: take the connection string from the project's Connect page and run pg_dump (Postgres), mysqldump (MySQL) or mongodump (MongoDB). You can start a new free project at any time and load your data into it, or move to a paid plan once they open.\n")
	if s.cfg.DashboardURL != "" {
		fmt.Fprintf(&b, "\nOpen the project: %s/#/project/%s\n", strings.TrimRight(s.cfg.DashboardURL, "/"), p.Ref)
	}
	b.WriteString("\n— FloBase\n")
	return s.cfg.Mail.send(to, subject, b.String())
}
