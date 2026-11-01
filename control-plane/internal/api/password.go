package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"flobase/control-plane/internal/catalog"
	"flobase/control-plane/internal/keys"
	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

// resetPassword gives a project's database a new random password, for when
// the old one leaked. Every user that had the old password gets the new
// one, the project's services restart with it on the same ports, and open
// connections end, so anyone holding the old password is locked out.
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Project(r.Context(), r.PathValue("ref"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Project not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	if p.Status != store.StatusActive {
		writeError(w, http.StatusConflict, "The project must be active to reset its password.")
		return
	}
	plan, ok := catalog.PlanByID(p.Plan)
	if !ok {
		s.internal(w, errors.New("unknown plan "+p.Plan))
		return
	}
	// A backup reads from the database container, which restarts here.
	if !s.jobs.start(p.Ref) {
		writeError(w, http.StatusConflict, "A backup of this project is running. Try again when it finishes.")
		return
	}
	defer s.jobs.done(p.Ref)
	// Finish even if the browser goes away: stopping halfway would leave
	// services on a password the database no longer accepts.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()

	old := p.DBPassword
	spec := provisioner.Spec{
		Ref: p.Ref, Engine: p.Engine, Plan: plan, User: p.DBUser, Password: keys.Password(), Database: p.DBName,
		JWTSecret: p.JWTSecret, AuthURL: s.apiURL(p.Ref) + "/auth/v1",
	}
	if err := s.prov.SetPassword(ctx, spec, old); err != nil {
		s.log.Error("reset password", "ref", p.Ref, "err", err)
		writeError(w, http.StatusBadGateway, "Could not change the password: "+err.Error())
		return
	}
	if err := s.store.SetPassword(ctx, p.Ref, spec.Password); err != nil {
		// Put the old password back so the record and the database agree.
		back := spec
		back.Password = old
		if err := s.prov.SetPassword(ctx, back, spec.Password); err != nil {
			s.log.Error("restore old password", "ref", p.Ref, "err", err)
		}
		s.internal(w, err)
		return
	}
	p.DBPassword = spec.Password
	if err := s.prov.Recreate(ctx, spec); err != nil {
		s.log.Error("restart after password reset", "ref", p.Ref, "err", err)
		writeError(w, http.StatusBadGateway, "The password changed, but the project's services did not restart: "+err.Error()+". Reset it again to retry.")
		return
	}
	s.log.Info("password reset", "ref", p.Ref)
	writeJSON(w, http.StatusOK, s.view(p, true))
}
