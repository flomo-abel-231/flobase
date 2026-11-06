package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"flobase/control-plane/internal/keys"
	"flobase/control-plane/internal/store"
)

// Team accounts work like Supabase Auth: signing in returns a short-lived
// JWT access token and a refresh token that is exchanged for a new pair
// before the access token runs out. Each refresh token works once. The
// dashboard keeps the refresh token in an httpOnly cookie and the access
// token in memory only. The install's API token keeps working as a key with
// the owner's rights, for the CLI, the desktop app and automations.

const (
	accessTTL     = time.Hour
	refreshTTL    = 30 * 24 * time.Hour
	inviteTTL     = 7 * 24 * time.Hour
	refreshCookie = "flobase_refresh"
	jwtAudience   = "flobase"
	minPassword   = 8
	// bcrypt reads at most 72 bytes of a password.
	maxPassword    = 72
	bcryptCost     = 12
	signInAttempts = 10
	attemptWindow  = 15 * time.Minute
)

type accountState struct {
	mu       sync.Mutex
	secret   string
	failures map[string][]time.Time
	// dummy is compared against when an email has no account, so a wrong
	// address takes as long as a wrong password.
	dummyOnce sync.Once
	dummy     []byte
}

// identity is who made a request: a signed-in user, or (User nil) the
// install's API token. Workspace is the workspace the request works in
// (empty for the API token used across all of them) and WorkspaceRole the
// caller's role there.
type identity struct {
	User          *store.User
	SessionID     string
	Workspace     string
	WorkspaceRole string
}

// role is the caller's role on the platform: "owner" runs this FloBase.
func (i identity) role() string {
	if i.User == nil {
		return "owner"
	}
	return i.User.Role
}

func (i identity) platformAdmin() bool { return i.role() == "owner" || i.role() == "admin" }

// allWorkspaces is the API token without a workspace header: the install's
// operator tools (CLI, desktop app) see every workspace.
func (i identity) allWorkspaces() bool { return i.User == nil && i.Workspace == "" }

func (i identity) canManageTeam() bool {
	return i.WorkspaceRole == "owner" || i.WorkspaceRole == "admin"
}

type identityKey struct{}

func who(r *http.Request) identity {
	id, _ := r.Context().Value(identityKey{}).(identity)
	return id
}

func (s *Server) accountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/v1/auth/setup", s.setupOwner)
	mux.HandleFunc("POST /api/v1/auth/signup", s.signUp)
	mux.HandleFunc("POST /api/v1/auth/token", s.issueToken)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /api/v1/auth/invites/{token}", s.inviteInfo)
	mux.HandleFunc("POST /api/v1/auth/invites/{token}/accept", s.acceptInvite)
	s.passwordRoutes(mux)
	mux.Handle("PATCH /api/v1/me", s.auth(s.updateMe))
	mux.Handle("POST /api/v1/me/password", s.auth(s.changePassword))
}

// workspaceHeader picks the workspace a request works in.
const workspaceHeader = "X-FloBase-Workspace"

// auth lets a request through with a valid API token or access token, and
// settles which workspace it works in. Requests about one project are
// allowed only for members of that project's workspace: nothing crosses
// from one workspace to another.
func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		id, err := s.identify(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Sign in again: the session has ended or the token is not valid.")
			return
		}
		if err := s.pickWorkspace(r, &id); errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusForbidden, "You are not a member of that workspace.")
			return
		} else if err != nil {
			s.internal(w, err)
			return
		}
		if strings.Contains(r.Pattern, " /api/v1/projects/{ref}") {
			ok, err := s.projectAccess(r, &id)
			if err != nil {
				s.internal(w, err)
				return
			}
			if !ok {
				writeError(w, http.StatusNotFound, "Project not found.")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}

// pickWorkspace sets the request's workspace: the one the header names,
// which the caller must belong to, else a person's first workspace.
func (s *Server) pickWorkspace(r *http.Request, id *identity) error {
	ws := strings.TrimSpace(r.Header.Get(workspaceHeader))
	if id.User == nil {
		if ws == "" {
			return nil
		}
		ok, err := s.store.WorkspaceExists(r.Context(), ws)
		if err != nil {
			return err
		}
		if !ok {
			return store.ErrNotFound
		}
		id.Workspace, id.WorkspaceRole = ws, "owner"
		return nil
	}
	if ws == "" {
		list, err := s.store.UserWorkspaces(r.Context(), id.User.ID)
		if err != nil || len(list) == 0 {
			return err
		}
		id.Workspace, id.WorkspaceRole = list[0].ID, list[0].Role
		return nil
	}
	role, err := s.store.MemberRole(r.Context(), ws, id.User.ID)
	if err != nil {
		return err
	}
	id.Workspace, id.WorkspaceRole = ws, role
	return nil
}

// projectAccess checks the caller may use the project in the path, and
// moves the request into that project's workspace.
func (s *Server) projectAccess(r *http.Request, id *identity) (bool, error) {
	p, err := s.store.Project(r.Context(), r.PathValue("ref"))
	if errors.Is(err, store.ErrNotFound) {
		// The handler answers 404 itself; nothing to reveal either way.
		return id.allWorkspaces(), nil
	} else if err != nil {
		return false, err
	}
	switch {
	case id.allWorkspaces():
		return true, nil
	case id.User == nil:
		return p.WorkspaceID == id.Workspace, nil
	}
	role, err := s.store.MemberRole(r.Context(), p.WorkspaceID, id.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	id.Workspace, id.WorkspaceRole = p.WorkspaceID, role
	return true, nil
}

// workspace is the request's workspace, answering the request when there
// is none (the API token across all workspaces, or someone in none).
func workspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	ws := who(r).Workspace
	if ws == "" {
		writeError(w, http.StatusBadRequest, "Pick a workspace: send its id in the "+workspaceHeader+" header.")
		return "", false
	}
	return ws, true
}

func (s *Server) teamAdmin(next http.HandlerFunc) http.Handler {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		if !who(r).canManageTeam() {
			writeError(w, http.StatusForbidden, "Only the workspace's owner and admins can do that.")
			return
		}
		next(w, r)
	})
}

// platformAdmin is for what runs the whole FloBase rather than one
// workspace: regions, backup storage, updates.
func (s *Server) platformAdmin(next http.HandlerFunc) http.Handler {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		if !who(r).platformAdmin() {
			writeError(w, http.StatusForbidden, "Only the people who run this FloBase can do that.")
			return
		}
		next(w, r)
	})
}

func (s *Server) identify(ctx context.Context, token string) (identity, error) {
	if token == "" {
		return identity{}, keys.ErrInvalidToken
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.APIToken)) == 1 {
		return identity{}, nil
	}
	secret, err := s.jwtSecret(ctx)
	if err != nil {
		return identity{}, err
	}
	claims, err := keys.Verify(secret, token)
	if err != nil {
		return identity{}, err
	}
	sid, _ := claims["session_id"].(string)
	sub, _ := claims["sub"].(string)
	if aud, _ := claims["aud"].(string); aud != jwtAudience || sid == "" {
		return identity{}, keys.ErrInvalidToken
	}
	// The session must still exist, so signing out and removing someone
	// take effect at once rather than when the access token expires.
	u, err := s.store.SessionUser(ctx, sid)
	if err != nil || u.ID != sub {
		return identity{}, keys.ErrInvalidToken
	}
	return identity{User: &u, SessionID: sid}, nil
}

// jwtSecret signs access tokens. It is made on first use and kept in the
// platform database, so tokens survive restarts and updates.
func (s *Server) jwtSecret(ctx context.Context) (string, error) {
	a := &s.accounts
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.secret != "" {
		return a.secret, nil
	}
	secret, err := s.store.Setting(ctx, "auth.jwt_secret", "")
	if err != nil {
		return "", err
	}
	if secret == "" {
		secret = keys.JWTSecret()
		if err := s.store.SetSetting(ctx, "auth.jwt_secret", secret); err != nil {
			return "", err
		}
	}
	a.secret = secret
	return secret, nil
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	// In the cloud anyone can sign up; a self-hosted FloBase is claimed
	// once, with its API token, and grows by invitation.
	writeJSON(w, http.StatusOK, map[string]bool{
		"setup_required": n == 0 && !s.billing(), "signup": s.billing(), "owner_email": s.cfg.OwnerEmail != "",
		"password_reset": s.cfg.Mail.enabled(),
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	id := who(r)
	var list []store.Workspace
	var err error
	if id.User == nil {
		list, err = s.store.AllWorkspaces(r.Context())
	} else {
		list, err = s.store.UserWorkspaces(r.Context(), id.User.ID)
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	out := map[string]any{"kind": "token", "role": id.role(), "workspaces": list, "workspace": id.Workspace}
	if id.User != nil {
		out["kind"], out["user"] = "user", id.User
	}
	writeJSON(w, http.StatusOK, out)
}

// setupOwner creates the first account. It needs the install's API token,
// or the address set as FLOBASE_OWNER_EMAIL, so on a public server nobody
// but its operator can claim it.
func (s *Server) setupOwner(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIToken string `json:"api_token"`
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !s.allowAttempt("setup") {
		writeError(w, http.StatusTooManyRequests, "Too many attempts. Wait 15 minutes and try again.")
		return
	}
	byEmail := strings.TrimSpace(req.APIToken) == "" && s.cfg.OwnerEmail != "" &&
		strings.EqualFold(strings.TrimSpace(req.Email), s.cfg.OwnerEmail)
	if !byEmail && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.APIToken)), []byte(s.cfg.APIToken)) != 1 {
		s.failAttempt("setup")
		msg := "That API token is not right. It is FLOBASE_API_TOKEN in the install's .env."
		if s.cfg.OwnerEmail != "" && strings.TrimSpace(req.APIToken) == "" {
			msg = "Use the owner email set in the install's .env (FLOBASE_OWNER_EMAIL), or enter the API token."
		}
		writeError(w, http.StatusUnauthorized, msg)
		return
	}
	email, name, ok := validAccount(w, req.Email, req.Name, req.Password)
	if !ok {
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		s.internal(w, err)
		return
	}
	u, err := s.store.CreateOwner(r.Context(), email, name, string(hash), workspaceName(name, email))
	if errors.Is(err, store.ErrExists) {
		writeError(w, http.StatusConflict, "This FloBase already has an owner. Sign in instead.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	s.log.Info("owner account created", "user", u.ID)
	s.startSession(w, r, u, http.StatusCreated)
}

// issueToken follows Supabase Auth's token endpoint: grant_type=password
// signs in, grant_type=refresh_token exchanges a refresh token.
func (s *Server) issueToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email        string `json:"email"`
		Password     string `json:"password"`
		RefreshToken string `json:"refresh_token"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	switch r.URL.Query().Get("grant_type") {
	case "password":
		s.signIn(w, r, strings.TrimSpace(req.Email), req.Password)
	case "refresh_token":
		token := req.RefreshToken
		if token == "" {
			if c, err := r.Cookie(refreshCookie); err == nil {
				token = c.Value
			}
		}
		s.refresh(w, r, token)
	default:
		writeError(w, http.StatusBadRequest, "grant_type must be password or refresh_token.")
	}
}

func (s *Server) signIn(w http.ResponseWriter, r *http.Request, email, password string) {
	key := "email:" + strings.ToLower(email)
	if !s.allowAttempt(key) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-in attempts for this email. Wait 15 minutes and try again.")
		return
	}
	u, err := s.store.UserByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internal(w, err)
		return
	}
	hash := []byte(u.PasswordHash)
	if err != nil {
		hash = s.dummyHash()
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || u.ID == "" {
		s.failAttempt(key)
		writeError(w, http.StatusBadRequest, "Wrong email or password.")
		return
	}
	s.clearAttempts(key)
	_ = s.store.DeleteExpiredSessions(r.Context())
	s.startSession(w, r, u, http.StatusOK)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request, token string) {
	if token == "" {
		writeError(w, http.StatusUnauthorized, "Not signed in.")
		return
	}
	next := keys.Token()
	sess, err := s.store.RotateSession(r.Context(), keys.Hash(token), keys.Hash(next), time.Now().Add(refreshTTL))
	if errors.Is(err, store.ErrReused) {
		s.log.Warn("refresh token used twice; session ended", "ip", r.RemoteAddr)
	}
	if err != nil {
		clearRefreshCookie(w, r)
		if errors.Is(err, store.ErrReused) || errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "The session has ended. Sign in again.")
		} else {
			s.internal(w, err)
		}
		return
	}
	u, err := s.store.User(r.Context(), sess.UserID)
	if err != nil {
		s.internal(w, err)
		return
	}
	s.respondSession(w, r, u, sess, next, http.StatusOK)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User, status int) {
	refresh := keys.Token()
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	sess, err := s.store.CreateSession(r.Context(), u.ID, keys.Hash(refresh), ua, time.Now().Add(refreshTTL))
	if err != nil {
		s.internal(w, err)
		return
	}
	s.respondSession(w, r, u, sess, refresh, status)
}

func (s *Server) respondSession(w http.ResponseWriter, r *http.Request, u store.User, sess store.Session, refresh string, status int) {
	secret, err := s.jwtSecret(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	now := time.Now()
	exp := now.Add(accessTTL)
	access := keys.Sign(secret, map[string]any{
		"iss": "flobase", "aud": jwtAudience, "sub": u.ID, "email": u.Email, "role": u.Role,
		"session_id": sess.ID, "iat": now.Unix(), "exp": exp.Unix(),
	})
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookie, Value: refresh, Path: "/api/v1/auth", Expires: sess.ExpiresAt,
		HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, status, map[string]any{
		"access_token": access, "token_type": "bearer", "expires_in": int(accessTTL.Seconds()), "expires_at": exp.Unix(),
		"refresh_token": refresh, "user": u,
	})
}

// logout ends the current session (scope=local, the default), every
// session of the user (global), or all but the current one (others). It
// also works with only the refresh cookie, once the access token expired.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	id, err := s.identify(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err == nil && id.User != nil {
		switch scope {
		case "global":
			err = s.store.DeleteUserSessions(r.Context(), id.User.ID, "")
		case "others":
			err = s.store.DeleteUserSessions(r.Context(), id.User.ID, id.SessionID)
		default:
			err = s.store.DeleteSession(r.Context(), id.SessionID)
		}
		if err != nil {
			s.internal(w, err)
			return
		}
	} else if c, cerr := r.Cookie(refreshCookie); cerr == nil && scope != "others" {
		_ = s.store.DeleteSessionByRefresh(r.Context(), keys.Hash(c.Value))
	}
	if scope != "others" {
		clearRefreshCookie(w, r)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) inviteInfo(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.InviteByToken(r.Context(), keys.Hash(r.PathValue("token")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "This invite link has expired or was already used. Ask for a new one.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	_, err = s.store.UserByEmail(r.Context(), inv.Email)
	writeJSON(w, http.StatusOK, map[string]any{
		"email": inv.Email, "role": inv.Role, "workspace": inv.WorkspaceName, "has_account": err == nil,
	})
}

func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	token := keys.Hash(r.PathValue("token"))
	inv, err := s.store.InviteByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "This invite link has expired or was already used. Ask for a new one.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	// Someone with an account joins with their password.
	if u, err := s.store.UserByEmail(r.Context(), inv.Email); err == nil {
		key := "email:" + strings.ToLower(u.Email)
		if !s.allowAttempt(key) {
			writeError(w, http.StatusTooManyRequests, "Too many attempts. Wait 15 minutes and try again.")
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
			s.failAttempt(key)
			writeError(w, http.StatusBadRequest, "Enter the password of your existing FloBase account.")
			return
		}
		if err := s.store.JoinByInvite(r.Context(), token, u.ID); errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "This invite link has expired or was already used. Ask for a new one.")
			return
		} else if err != nil {
			s.internal(w, err)
			return
		}
		s.log.Info("invite accepted", "user", u.ID, "workspace", inv.WorkspaceID)
		s.startSession(w, r, u, http.StatusOK)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		s.internal(w, err)
		return
	}
	_, name, ok := validAccount(w, "invited@flobase", req.Name, req.Password)
	if !ok {
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		s.internal(w, err)
		return
	}
	u, err := s.store.AcceptInvite(r.Context(), token, name, string(hash))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "This invite link has expired or was already used. Ask for a new one.")
		return
	case errors.Is(err, store.ErrExists):
		writeError(w, http.StatusConflict, "This email already has an account. Sign in instead.")
		return
	case err != nil:
		s.internal(w, err)
		return
	}
	s.log.Info("invite accepted", "user", u.ID, "workspace", inv.WorkspaceID)
	s.startSession(w, r, u, http.StatusCreated)
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	id := who(r)
	if id.User == nil {
		writeError(w, http.StatusBadRequest, "Signed in with the API token, which has no profile.")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if len(name) > 100 {
		writeError(w, http.StatusBadRequest, "Name must be at most 100 characters.")
		return
	}
	if err := s.store.SetUserName(r.Context(), id.User.ID, name); err != nil {
		s.internal(w, err)
		return
	}
	u := *id.User
	u.Name = name
	writeJSON(w, http.StatusOK, u)
}

// changePassword also signs out every other session, so a password change
// locks out anyone who had it.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	id := who(r)
	if id.User == nil {
		writeError(w, http.StatusBadRequest, "Signed in with the API token, which has no password.")
		return
	}
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decode(w, r, &req) {
		return
	}
	key := "email:" + strings.ToLower(id.User.Email)
	if !s.allowAttempt(key) {
		writeError(w, http.StatusTooManyRequests, "Too many attempts. Wait 15 minutes and try again.")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(id.User.PasswordHash), []byte(req.Current)) != nil {
		s.failAttempt(key)
		writeError(w, http.StatusBadRequest, "The current password is not right.")
		return
	}
	if _, _, ok := validAccount(w, id.User.Email, id.User.Name, req.New); !ok {
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.New), bcryptCost)
	if err != nil {
		s.internal(w, err)
		return
	}
	if err := s.store.SetUserPassword(r.Context(), id.User.ID, string(hash)); err != nil {
		s.internal(w, err)
		return
	}
	if err := s.store.DeleteUserSessions(r.Context(), id.User.ID, id.SessionID); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validAccount checks an email, name and password, answering the request
// when one is not acceptable.
func validAccount(w http.ResponseWriter, email, name, password string) (string, string, bool) {
	email = strings.TrimSpace(email)
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 {
		writeError(w, http.StatusBadRequest, "Enter a valid email address.")
		return "", "", false
	}
	name = strings.TrimSpace(name)
	if len(name) > 100 {
		writeError(w, http.StatusBadRequest, "Name must be at most 100 characters.")
		return "", "", false
	}
	if len(password) < minPassword || len(password) > maxPassword {
		writeError(w, http.StatusBadRequest, "Password must be 8 to 72 characters.")
		return "", "", false
	}
	return email, name, true
}

func (s *Server) dummyHash() []byte {
	a := &s.accounts
	a.dummyOnce.Do(func() {
		a.dummy, _ = bcrypt.GenerateFromPassword([]byte(keys.Token()), bcryptCost)
	})
	return a.dummy
}

func (s *Server) allowAttempt(key string) bool { return s.allowAttempts(key, signInAttempts) }

// allowAttempts is whether key has had fewer than max failures in the
// window.
func (s *Server) allowAttempts(key string, max int) bool {
	a := &s.accounts
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := time.Now().Add(-attemptWindow)
	recent := a.failures[key][:0]
	for _, t := range a.failures[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(a.failures, key)
	} else {
		a.failures[key] = recent
	}
	return len(recent) < max
}

func (s *Server) failAttempt(key string) {
	a := &s.accounts
	a.mu.Lock()
	if a.failures == nil {
		a.failures = map[string][]time.Time{}
	}
	a.failures[key] = append(a.failures[key], time.Now())
	a.mu.Unlock()
}

func (s *Server) clearAttempts(key string) {
	a := &s.accounts
	a.mu.Lock()
	delete(a.failures, key)
	a.mu.Unlock()
}

// secureRequest is true for HTTPS, directly or behind a proxy that says so.
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func clearRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookie, Value: "", Path: "/api/v1/auth", MaxAge: -1,
		HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteStrictMode,
	})
}
