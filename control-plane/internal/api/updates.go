package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Updates: the control plane asks GitHub Releases for the newest FloBase
// every few hours. With auto-update on, and FloBase installed by the
// install script (so upgrade.sh and the compose file live in InstallDir),
// it starts the updater; otherwise the dashboard offers the update.

const updateEvery = 6 * time.Hour

// Updater is what the provisioner offers for self-updates.
type Updater interface {
	StartUpdater(ctx context.Context, installDir, version, repo string) error
	UpdaterRunning(ctx context.Context) bool
}

type updateState struct {
	mu        sync.Mutex
	latest    string
	notes     string
	url       string
	checked   time.Time
	lastError string
}

type updateView struct {
	Current     string     `json:"current"`
	Latest      string     `json:"latest,omitempty"`
	Available   bool       `json:"available"`
	Notes       string     `json:"notes,omitempty"`
	ReleaseURL  string     `json:"release_url,omitempty"`
	Auto        bool       `json:"auto"`
	CanUpdate   bool       `json:"can_update"`
	Updating    bool       `json:"updating"`
	CheckedAt   *time.Time `json:"checked_at,omitempty"`
	Error       string     `json:"error,omitempty"`
	ManualSteps string     `json:"manual_steps,omitempty"`
}

func (s *Server) updateRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/updates", s.auth(s.getUpdates))
	mux.Handle("POST /api/v1/updates/check", s.auth(s.checkUpdatesNow))
	mux.Handle("POST /api/v1/updates", s.auth(s.updateNow))
}

// canUpdate: the updater needs the install directory, and a dev build has
// no version to compare.
func (s *Server) canUpdate() bool {
	_, ok := parseVersion(s.cfg.Version)
	return s.cfg.InstallDir != "" && s.updater != nil && ok
}

// RunUpdateChecks checks now and then every few hours.
func (s *Server) RunUpdateChecks(ctx context.Context) {
	for {
		if err := s.checkUpdates(ctx); err != nil {
			s.log.Warn("update check", "err", err)
		} else if v := s.updates.snapshot(); s.cfg.AutoUpdate && s.canUpdate() && newer(v.latest, s.cfg.Version) {
			s.log.Info("auto-update starting", "from", s.cfg.Version, "to", v.latest)
			if err := s.updater.StartUpdater(ctx, s.cfg.InstallDir, v.latest, s.cfg.UpdateRepo); err != nil {
				s.log.Error("auto-update", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(updateEvery):
		}
	}
}

func (u *updateState) snapshot() updateState {
	u.mu.Lock()
	defer u.mu.Unlock()
	return updateState{latest: u.latest, notes: u.notes, url: u.url, checked: u.checked, lastError: u.lastError}
}

// checkUpdates reads the newest published release (drafts and pre-releases
// are skipped by GitHub's "latest").
func (s *Server) checkUpdates(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+s.cfg.UpdateRepo+"/releases/latest", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "flobase/"+s.cfg.Version)
	resp, err := http.DefaultClient.Do(req)
	if err == nil && resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("GitHub answered %s", resp.Status)
		resp.Body.Close()
	}
	s.updates.mu.Lock()
	defer s.updates.mu.Unlock()
	s.updates.checked = time.Now().UTC()
	if err != nil {
		s.updates.lastError = err.Error()
		return err
	}
	defer resp.Body.Close()
	var rel struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		s.updates.lastError = err.Error()
		return err
	}
	s.updates.latest = strings.TrimPrefix(rel.TagName, "v")
	s.updates.notes, s.updates.url, s.updates.lastError = rel.Body, rel.HTMLURL, ""
	return nil
}

func (s *Server) updateView(ctx context.Context) updateView {
	st := s.updates.snapshot()
	v := updateView{
		Current: s.cfg.Version, Latest: st.latest, Available: newer(st.latest, s.cfg.Version),
		Notes: st.notes, ReleaseURL: st.url, Auto: s.cfg.AutoUpdate, CanUpdate: s.canUpdate(), Error: st.lastError,
	}
	if !st.checked.IsZero() {
		v.CheckedAt = &st.checked
	}
	if s.updater != nil {
		v.Updating = s.updater.UpdaterRunning(ctx)
	}
	if v.Available && !v.CanUpdate {
		v.ManualSteps = "cd <install dir> && sh upgrade.sh " + st.latest
	}
	return v
}

func (s *Server) getUpdates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.updateView(r.Context()))
}

func (s *Server) checkUpdatesNow(w http.ResponseWriter, r *http.Request) {
	if err := s.checkUpdates(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, "Could not reach GitHub: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.updateView(r.Context()))
}

func (s *Server) updateNow(w http.ResponseWriter, r *http.Request) {
	v := s.updateView(r.Context())
	if !v.Available {
		writeError(w, http.StatusConflict, "FloBase is up to date.")
		return
	}
	if !v.CanUpdate {
		writeError(w, http.StatusConflict, "This FloBase was not installed with the install script, so update it by hand: "+v.ManualSteps)
		return
	}
	if err := s.updater.StartUpdater(r.Context(), s.cfg.InstallDir, v.Latest, s.cfg.UpdateRepo); err != nil {
		writeError(w, http.StatusConflict, "Could not start the update: "+err.Error())
		return
	}
	s.log.Info("update started from the dashboard", "from", s.cfg.Version, "to", v.Latest)
	w.WriteHeader(http.StatusAccepted)
}

// parseVersion reads "1.2.3"; anything else (such as "dev") is not a
// release.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newer reports whether release a is later than b.
func newer(a, b string) bool {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}
