package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"flobase/control-plane/internal/keys"
	"flobase/control-plane/internal/store"
)

// Forgot password: someone asks for a reset link with their email, FloBase
// emails a one-time link (through FLOBASE_SMTP_*) that is good for an
// hour, and opening it lets them set a new password, which signs out every
// other session. The answer to a request is the same whether or not the
// address has an account, so it cannot be used to find out who has one.

const (
	resetTTL = time.Hour
	// At most this many reset emails to one address in 15 minutes.
	resetsPerAddress = 3
)

func (s *Server) passwordRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/recover", s.recoverPassword)
	mux.HandleFunc("GET /api/v1/auth/reset/{token}", s.resetInfo)
	mux.HandleFunc("POST /api/v1/auth/reset/{token}", s.setForgottenPassword)
}

const resetSent = "If that address has a FloBase account, a link to set a new password is on its way. It works for an hour."

func (s *Server) recoverPassword(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Mail.enabled() {
		writeError(w, http.StatusServiceUnavailable, "Password reset by email is not set up on this FloBase. Ask the person who runs it to set your password.")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || len(email) > 254 {
		writeError(w, http.StatusBadRequest, "Enter your email address.")
		return
	}
	// Every request counts, by address and by client, so nobody can flood
	// an inbox or try many addresses.
	byEmail, byClient := "recover:"+strings.ToLower(email), "recover-ip:"+clientIP(r)
	if !s.allowAttempts(byEmail, resetsPerAddress) || !s.allowAttempt(byClient) {
		writeError(w, http.StatusTooManyRequests, "Too many requests. Wait 15 minutes and try again.")
		return
	}
	s.failAttempt(byEmail)
	s.failAttempt(byClient)

	u, err := s.store.UserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]string{"message": resetSent})
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	token := keys.Token()
	if err := s.store.CreatePasswordReset(r.Context(), u.ID, keys.Hash(token), time.Now().Add(resetTTL)); err != nil {
		s.internal(w, err)
		return
	}
	link := s.dashboardBase(r) + "/#/reset/" + token
	// Send in the background, so the answer takes as long for an address
	// with an account as for one without.
	go func() {
		if err := s.cfg.Mail.send([]string{u.Email}, "Reset your FloBase password", resetEmail(u, link)); err != nil {
			s.log.Warn("password reset email", "user", u.ID, "err", err)
			return
		}
		s.log.Info("password reset email sent", "user", u.ID)
	}()
	writeJSON(w, http.StatusOK, map[string]string{"message": resetSent})
}

func resetEmail(u store.User, link string) string {
	name := u.Name
	if name == "" {
		name = "there"
	}
	return fmt.Sprintf(`Hi %s,

Someone asked to reset the password of your FloBase account (%s). To choose a new password, open this link within an hour:

%s

If it wasn't you, ignore this email: your password stays as it is.

— FloBase
`, name, u.Email, link)
}

// dashboardBase is where links in emails point: FLOBASE_DASHBOARD_URL, or
// else the address this request came to.
func (s *Server) dashboardBase(r *http.Request) string {
	if s.cfg.DashboardURL != "" {
		return strings.TrimRight(s.cfg.DashboardURL, "/")
	}
	scheme := "http"
	if secureRequest(r) {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

func (s *Server) resetInfo(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.PasswordResetUser(r.Context(), keys.Hash(r.PathValue("token")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "This reset link has expired or was already used. Ask for a new one.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"email": u.Email})
}

// setForgottenPassword sets a new password from a reset link and signs in.
func (s *Server) setForgottenPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	token := keys.Hash(r.PathValue("token"))
	u, err := s.store.PasswordResetUser(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "This reset link has expired or was already used. Ask for a new one.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	if _, _, ok := validAccount(w, u.Email, u.Name, req.Password); !ok {
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		s.internal(w, err)
		return
	}
	u, err = s.store.ResetPassword(r.Context(), token, string(hash))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "This reset link has expired or was already used. Ask for a new one.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	// A new password ends any lockout from wrong guesses.
	s.clearAttempts("email:" + strings.ToLower(u.Email))
	_ = s.store.DeleteExpiredPasswordResets(r.Context())
	s.log.Info("password reset", "user", u.ID)
	s.startSession(w, r, u, http.StatusOK)
}
