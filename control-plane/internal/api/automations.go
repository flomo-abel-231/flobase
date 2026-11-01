package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"flobase/control-plane/internal/store"
)

// Automations run the assistant on a prompt every hour, day or week, such as
// "check every project's backups and report any that failed". Each run is a
// conversation of its own. Nobody is there to approve anything, so a run only
// does what the permission setting allows without asking.

func (s *Server) automationRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/automations", s.auth(s.listAutomations))
	mux.Handle("POST /api/v1/automations", s.auth(s.createAutomation))
	mux.Handle("PATCH /api/v1/automations/{id}", s.auth(s.updateAutomation))
	mux.Handle("POST /api/v1/automations/{id}/run", s.auth(s.runAutomationNow))
	mux.Handle("DELETE /api/v1/automations/{id}", s.auth(s.deleteAutomation))
}

var intervals = map[string]time.Duration{"hour": time.Hour, "day": 24 * time.Hour, "week": 7 * 24 * time.Hour}

type automationView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prompt     string     `json:"prompt"`
	ProviderID *string    `json:"provider_id"`
	Every      string     `json:"every"`
	Enabled    bool       `json:"enabled"`
	NextRunAt  time.Time  `json:"next_run_at"`
	LastRunAt  *time.Time `json:"last_run_at"`
	LastChatID *string    `json:"last_chat_id"`
	LastStatus string     `json:"last_status,omitempty"`
}

func (s *Server) viewAutomation(ctx context.Context, a store.Automation) automationView {
	v := automationView{ID: a.ID, Name: a.Name, Prompt: a.Prompt, ProviderID: a.ProviderID, Every: a.Every,
		Enabled: a.Enabled, NextRunAt: a.NextRunAt, LastRunAt: a.LastRunAt, LastChatID: a.LastChatID}
	if a.LastChatID != nil {
		if c, err := s.store.Chat(ctx, a.WorkspaceID, *a.LastChatID); err == nil {
			v.LastStatus = c.Status
		}
	}
	return v
}

func (s *Server) listAutomations(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	list, err := s.store.Automations(r.Context(), ws)
	if err != nil {
		s.internal(w, err)
		return
	}
	views := make([]automationView, 0, len(list))
	for _, a := range list {
		views = append(views, s.viewAutomation(r.Context(), a))
	}
	writeJSON(w, http.StatusOK, views)
}

type automationRequest struct {
	Name       string `json:"name"`
	Prompt     string `json:"prompt"`
	ProviderID string `json:"provider_id"`
	Every      string `json:"every"`
	Enabled    *bool  `json:"enabled"`
}

func (s *Server) automation(ctx context.Context, req automationRequest, a store.Automation) (store.Automation, error) {
	a.Name, a.Prompt, a.Every = strings.TrimSpace(req.Name), strings.TrimSpace(req.Prompt), req.Every
	a.ProviderID = nil
	if req.ProviderID != "" {
		if _, err := s.store.LLMProvider(ctx, a.WorkspaceID, req.ProviderID); err != nil {
			return a, errors.New("Choose a model that is still set up.")
		}
		a.ProviderID = &req.ProviderID
	}
	if req.Enabled != nil {
		a.Enabled = *req.Enabled
	}
	switch {
	case utf8.RuneCountInString(a.Name) < 1 || utf8.RuneCountInString(a.Name) > 64:
		return a, errors.New("Give the automation a name of up to 64 characters.")
	case a.Prompt == "" || len(a.Prompt) > 8<<10:
		return a, errors.New("Say what the automation should do, in up to 8 KB.")
	case intervals[a.Every] == 0:
		return a, errors.New("Choose how often it runs: every hour, day or week.")
	}
	return a, nil
}

func (s *Server) createAutomation(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	var req automationRequest
	if !decode(w, r, &req) {
		return
	}
	a, err := s.automation(r.Context(), req, store.Automation{WorkspaceID: ws, Enabled: true})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.NextRunAt = time.Now().Add(intervals[a.Every])
	saved, err := s.store.CreateAutomation(r.Context(), a)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.viewAutomation(r.Context(), saved))
}

func (s *Server) updateAutomation(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	var req automationRequest
	if !decode(w, r, &req) {
		return
	}
	cur, err := s.store.Automation(r.Context(), ws, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Automation not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	a, err := s.automation(r.Context(), req, cur)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.Every != cur.Every || (a.Enabled && !cur.Enabled) {
		a.NextRunAt = time.Now().Add(intervals[a.Every])
	}
	if err := s.store.UpdateAutomation(r.Context(), a); err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.viewAutomation(r.Context(), a))
}

func (s *Server) deleteAutomation(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteAutomation(r.Context(), ws, r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Automation not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runAutomationNow(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	a, err := s.store.Automation(r.Context(), ws, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Automation not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	c, err := s.startAutomation(r.Context(), a)
	if errors.Is(err, errNoModel) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, viewChat(c))
}

// startAutomation starts a run and schedules the next one.
func (s *Server) startAutomation(ctx context.Context, a store.Automation) (store.Chat, error) {
	provider := ""
	if a.ProviderID != nil {
		provider = *a.ProviderID
	}
	c, err := s.newChat(ctx, a.WorkspaceID, provider, &a.ID, a.Prompt)
	if err != nil {
		return c, err
	}
	c.Title = a.Name + " · " + time.Now().UTC().Format("2 Jan 15:04") + " UTC"
	if err := s.store.RenameChat(ctx, c.ID, c.Title); err != nil {
		return c, err
	}
	if err := s.store.AutomationRan(ctx, a.ID, c.ID, time.Now().Add(intervals[a.Every])); err != nil {
		return c, err
	}
	s.startChat(c.ID, true)
	return c, nil
}

// RunAutomations starts automations when they are due.
func (s *Server) RunAutomations(ctx context.Context) {
	if err := s.store.FailRunningChats(ctx); err != nil {
		s.log.Error("mark interrupted conversations", "err", err)
	}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		due, err := s.store.DueAutomations(ctx)
		if err != nil {
			s.log.Error("read automations", "err", err)
			continue
		}
		for _, a := range due {
			if _, err := s.startAutomation(ctx, a); err != nil {
				s.log.Error("start automation", "name", a.Name, "err", err)
				// Try again at the next interval, not every minute.
				_ = s.store.AutomationRan(ctx, a.ID, "", time.Now().Add(intervals[a.Every]))
			}
		}
	}
}
