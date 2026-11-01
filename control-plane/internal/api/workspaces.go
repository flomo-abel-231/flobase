package api

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"flobase/control-plane/internal/keys"
	"flobase/control-plane/internal/store"
)

// Workspaces: each is its own organization, with its own members,
// projects, invites and assistant. People can belong to several and switch
// between them; nothing is shared between workspaces. In the cloud anyone
// can sign up and gets a workspace of their own.

func (s *Server) workspaceRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/workspaces", s.auth(s.me))
	mux.Handle("POST /api/v1/workspaces", s.auth(s.createWorkspace))
	mux.Handle("PATCH /api/v1/workspaces/{id}", s.auth(s.renameWorkspace))
	mux.Handle("DELETE /api/v1/workspaces/{id}", s.auth(s.deleteWorkspace))
	// The team is the current workspace's members and invites.
	mux.Handle("GET /api/v1/team", s.auth(s.team))
	mux.Handle("POST /api/v1/team/invites", s.teamAdmin(s.invite))
	mux.Handle("DELETE /api/v1/team/invites/{id}", s.teamAdmin(s.deleteInvite))
	mux.Handle("PATCH /api/v1/team/users/{id}", s.teamAdmin(s.setRole))
	mux.Handle("DELETE /api/v1/team/users/{id}", s.teamAdmin(s.removeUser))
}

// workspaceName is the name a new account's first workspace gets.
func workspaceName(name, email string) string {
	if name == "" {
		name, _, _ = strings.Cut(email, "@")
	}
	return name + "'s workspace"
}

// clientIP is the address a request came from, as the proxy in front saw
// it. It only feeds the sign-up rate limit.
func clientIP(r *http.Request) string {
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		first, _, _ := strings.Cut(f, ",")
		return strings.TrimSpace(first)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// signUp creates an account and its own workspace (cloud mode). The person
// who runs this FloBase becomes the platform owner: the address in
// FLOBASE_OWNER_EMAIL, or else the first account.
func (s *Server) signUp(w http.ResponseWriter, r *http.Request) {
	if !s.billing() {
		writeError(w, http.StatusForbidden, "Sign-up is closed here. Ask the owner for an invite.")
		return
	}
	var req struct {
		Email     string `json:"email"`
		Name      string `json:"name"`
		Password  string `json:"password"`
		Workspace string `json:"workspace"`
	}
	if !decode(w, r, &req) {
		return
	}
	key := "signup:" + clientIP(r)
	if !s.allowAttempt(key) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-ups from here. Wait 15 minutes and try again.")
		return
	}
	email, name, ok := validAccount(w, req.Email, req.Name, req.Password)
	if !ok {
		return
	}
	wsName := strings.TrimSpace(req.Workspace)
	if wsName == "" {
		wsName = workspaceName(name, email)
	}
	if utf8.RuneCountInString(wsName) > 64 {
		writeError(w, http.StatusBadRequest, "The workspace name is at most 64 characters.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		s.internal(w, err)
		return
	}
	role := "member"
	if owner := s.cfg.OwnerEmail; owner != "" {
		if strings.EqualFold(owner, email) {
			role = "owner"
		}
	} else if n, err := s.store.CountUsers(r.Context()); err == nil && n == 0 {
		role = "owner"
	}
	s.failAttempt(key) // every sign-up counts toward the limit
	u, ws, err := s.store.SignUp(r.Context(), email, name, string(hash), role, wsName)
	if errors.Is(err, store.ErrExists) && role == "owner" {
		// Someone else became the owner a moment earlier.
		u, ws, err = s.store.SignUp(r.Context(), email, name, string(hash), "member", wsName)
	}
	if errors.Is(err, store.ErrExists) {
		writeError(w, http.StatusConflict, "This email already has an account. Sign in instead.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	s.log.Info("account created", "user", u.ID, "workspace", ws.ID, "platform_role", u.Role)
	s.startSession(w, r, u, http.StatusCreated)
}

func validWorkspaceName(w http.ResponseWriter, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
		writeError(w, http.StatusBadRequest, "The workspace name is 1 to 64 characters.")
		return "", false
	}
	return name, true
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	u := who(r).User
	if u == nil {
		writeError(w, http.StatusBadRequest, "Sign in as a person to create a workspace; the API token has no account to own it.")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name, ok := validWorkspaceName(w, req.Name)
	if !ok {
		return
	}
	ws, err := s.store.CreateWorkspace(r.Context(), name, u.ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

// workspaceRole is the caller's role in the workspace named in the path.
func (s *Server) workspaceRole(r *http.Request) (string, error) {
	id := who(r)
	ws := r.PathValue("id")
	if id.User == nil {
		if ok, err := s.store.WorkspaceExists(r.Context(), ws); err != nil || !ok {
			return "", errors.Join(err, store.ErrNotFound)
		}
		return "owner", nil
	}
	return s.store.MemberRole(r.Context(), ws, id.User.ID)
}

func (s *Server) renameWorkspace(w http.ResponseWriter, r *http.Request) {
	role, err := s.workspaceRole(r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Workspace not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	if role != "owner" && role != "admin" {
		writeError(w, http.StatusForbidden, "Only the workspace's owner and admins can rename it.")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name, ok := validWorkspaceName(w, req.Name)
	if !ok {
		return
	}
	if err := s.store.RenameWorkspace(r.Context(), r.PathValue("id"), name); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	role, err := s.workspaceRole(r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Workspace not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	if role != "owner" {
		writeError(w, http.StatusForbidden, "Only the workspace's owner can delete it.")
		return
	}
	switch err := s.store.DeleteWorkspace(r.Context(), r.PathValue("id")); {
	case errors.Is(err, store.ErrWorkspaceInUse):
		writeError(w, http.StatusConflict, "Delete the workspace's projects first.")
	case err != nil:
		s.internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) team(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	members, err := s.store.Members(r.Context(), ws)
	if err != nil {
		s.internal(w, err)
		return
	}
	invites := []store.Invite{}
	if who(r).canManageTeam() {
		if invites, err = s.store.Invites(r.Context(), ws); err != nil {
			s.internal(w, err)
			return
		}
	}
	if invites == nil {
		invites = []store.Invite{}
	}
	// The team page shows each person's role in this workspace.
	users := make([]store.User, 0, len(members))
	for _, m := range members {
		u := m.User
		u.Role = m.WorkspaceRole
		users = append(users, u)
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users, "invites": invites})
}

// invite returns the invite's token once; the dashboard turns it into a
// link to send. FloBase does not send email yet.
func (s *Server) invite(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	email, _, ok := validAccount(w, req.Email, "", "placeholder")
	if !ok {
		return
	}
	if req.Role != "admin" && req.Role != "member" {
		writeError(w, http.StatusBadRequest, "Role must be admin or member.")
		return
	}
	invitedBy := ""
	if u := who(r).User; u != nil {
		invitedBy = u.ID
	}
	token := keys.Token()
	inv, err := s.store.CreateInvite(r.Context(), ws, email, req.Role, keys.Hash(token), invitedBy, time.Now().Add(inviteTTL))
	if errors.Is(err, store.ErrExists) {
		writeError(w, http.StatusConflict, "That person is already in this workspace.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"invite": inv, "token": token})
}

func (s *Server) deleteInvite(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteInvite(r.Context(), ws, r.PathValue("id")); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setRole(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Role != "admin" && req.Role != "member" {
		writeError(w, http.StatusBadRequest, "Role must be admin or member.")
		return
	}
	if u := who(r).User; u != nil && u.ID == r.PathValue("id") {
		writeError(w, http.StatusBadRequest, "You cannot change your own role.")
		return
	}
	if err := s.store.SetMemberRole(r.Context(), ws, r.PathValue("id"), req.Role); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "No such person in this workspace, or it is its owner, whose role stays.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removeUser takes someone out of the workspace; their account and other
// workspaces stay.
func (s *Server) removeUser(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace(w, r)
	if !ok {
		return
	}
	if u := who(r).User; u != nil && u.ID == r.PathValue("id") {
		writeError(w, http.StatusBadRequest, "You cannot remove yourself.")
		return
	}
	if err := s.store.RemoveMember(r.Context(), ws, r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "No such person in this workspace, or it is its owner, who cannot be removed.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
