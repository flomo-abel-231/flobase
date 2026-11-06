package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrExists is returned when an email address already has an account or
// an invite.
var ErrExists = errors.New("already exists")

// ErrReused is returned when a refresh token that was already exchanged is
// used again; the session it belonged to is ended.
var ErrReused = errors.New("refresh token reused")

// User is a person who signs in to the dashboard.
type User struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"`
	CreatedAt    time.Time  `json:"created_at"`
	LastSignInAt *time.Time `json:"last_sign_in_at"`
}

const userColumns = `id, email, name, password_hash, role, created_at, last_sign_in_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.LastSignInAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `select count(*) from users`).Scan(&n)
	return n, err
}

// CreateOwner adds the first account, the platform owner, with its
// workspace. It fails with ErrExists once any account exists, so two
// people setting up at once cannot both win.
func (s *Store) CreateOwner(ctx context.Context, email, name, hash, workspace string) (User, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	u, err := scanUser(tx.QueryRow(ctx, `
		insert into users (email, name, password_hash, role)
		select $1, $2, $3, 'owner' where not exists (select 1 from users)
		returning `+userColumns, email, name, hash))
	if errors.Is(err, ErrNotFound) || isUnique(err) {
		return u, ErrExists
	} else if err != nil {
		return u, err
	}
	if _, err := ownerWorkspace(ctx, tx, workspace, u.ID); err != nil {
		return u, err
	}
	return u, tx.Commit(ctx)
}

func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.Query(ctx, `select `+userColumns+` from users order by created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) User(ctx context.Context, id string) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `select `+userColumns+` from users where id = $1`, id))
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `select `+userColumns+` from users where lower(email) = lower($1)`, email))
}

func (s *Store) SetUserName(ctx context.Context, id, name string) error {
	_, err := s.db.Exec(ctx, `update users set name = $2 where id = $1`, id, name)
	return err
}

func (s *Store) SetUserPassword(ctx context.Context, id, hash string) error {
	_, err := s.db.Exec(ctx, `update users set password_hash = $2 where id = $1`, id, hash)
	return err
}

// SetUserRole changes an admin or member; the owner keeps their role.
func (s *Store) SetUserRole(ctx context.Context, id, role string) error {
	tag, err := s.db.Exec(ctx, `update users set role = $2 where id = $1 and role <> 'owner'`, id, role)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DeleteUser removes an admin or member and, with them, their sessions.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, `delete from users where id = $1 and role <> 'owner'`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Session is one signed-in client.
type Session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
}

func (s *Store) CreateSession(ctx context.Context, userID, refreshHash, userAgent string, expires time.Time) (Session, error) {
	sess := Session{UserID: userID, ExpiresAt: expires}
	err := s.db.QueryRow(ctx, `
		insert into sessions (user_id, refresh_hash, user_agent, expires_at) values ($1, $2, $3, $4) returning id`,
		userID, refreshHash, userAgent, expires).Scan(&sess.ID)
	if err == nil {
		_, err = s.db.Exec(ctx, `update users set last_sign_in_at = now() where id = $1`, userID)
	}
	return sess, err
}

// SessionUser is the user a live session belongs to.
func (s *Store) SessionUser(ctx context.Context, sessionID string) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `
		select `+prefixed("u.", userColumns)+` from sessions s join users u on u.id = s.user_id
		where s.id = $1 and s.expires_at > now()`, sessionID))
}

// RotateSession exchanges a refresh token for a new one. A token that was
// already exchanged ends its session with ErrReused, unless it was
// exchanged in the last few seconds (two requests racing, not a thief).
func (s *Store) RotateSession(ctx context.Context, oldHash, newHash string, expires time.Time) (Session, error) {
	sess := Session{ExpiresAt: expires}
	err := s.db.QueryRow(ctx, `
		update sessions set previous_hash = refresh_hash, refresh_hash = $2, refreshed_at = now(), expires_at = $3
		where refresh_hash = $1 and expires_at > now()
		returning id, user_id`, oldHash, newHash, expires).Scan(&sess.ID, &sess.UserID)
	if !errors.Is(err, pgx.ErrNoRows) {
		return sess, err
	}
	tag, err := s.db.Exec(ctx, `
		delete from sessions where previous_hash = $1 and refreshed_at < now() - interval '10 seconds'`, oldHash)
	if err != nil {
		return sess, err
	}
	if tag.RowsAffected() > 0 {
		return sess, ErrReused
	}
	return sess, ErrNotFound
}

// DeleteSessionByRefresh signs out the session a refresh token belongs to.
func (s *Store) DeleteSessionByRefresh(ctx context.Context, refreshHash string) error {
	_, err := s.db.Exec(ctx, `delete from sessions where refresh_hash = $1`, refreshHash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `delete from sessions where id = $1`, id)
	return err
}

// DeleteUserSessions ends a user's sessions, all but keep (empty = all).
func (s *Store) DeleteUserSessions(ctx context.Context, userID, keep string) error {
	_, err := s.db.Exec(ctx, `delete from sessions where user_id = $1 and id::text <> $2`, userID, keep)
	return err
}

// DeleteExpiredSessions clears sessions nobody can refresh any more.
func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `delete from sessions where expires_at < now()`)
	return err
}

// Invite asks someone to join a workspace with a given role. Someone
// without an account creates one when accepting.
type Invite struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

const inviteColumns = `i.id, i.workspace_id::text, w.name, i.email, i.role, i.created_at, i.expires_at`

func scanInvite(row pgx.Row) (Invite, error) {
	var i Invite
	err := row.Scan(&i.ID, &i.WorkspaceID, &i.WorkspaceName, &i.Email, &i.Role, &i.CreatedAt, &i.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return i, ErrNotFound
	}
	return i, err
}

// CreateInvite replaces an earlier invite to the same address in the same
// workspace, so inviting again gives a fresh link. Someone already in the
// workspace is ErrExists.
func (s *Store) CreateInvite(ctx context.Context, ws, email, role, tokenHash, invitedBy string, expires time.Time) (Invite, error) {
	var member bool
	if err := s.db.QueryRow(ctx, `select exists (select 1 from workspace_members m join users u on u.id = m.user_id
		where m.workspace_id::text = $1 and lower(u.email) = lower($2))`, ws, email).Scan(&member); err != nil {
		return Invite{}, err
	}
	if member {
		return Invite{}, ErrExists
	}
	return scanInvite(s.db.QueryRow(ctx, `
		with i as (
			insert into invites (workspace_id, email, role, token_hash, invited_by, expires_at)
			values ($1::uuid, $2, $3, $4, nullif($5, '')::uuid, $6)
			on conflict (workspace_id, lower(email)) do update set role = excluded.role, token_hash = excluded.token_hash,
				invited_by = excluded.invited_by, created_at = now(), expires_at = excluded.expires_at
			returning *)
		select `+inviteColumns+` from i join workspaces w on w.id = i.workspace_id`,
		ws, email, role, tokenHash, invitedBy, expires))
}

func (s *Store) Invites(ctx context.Context, ws string) ([]Invite, error) {
	rows, err := s.db.Query(ctx, `select `+inviteColumns+` from invites i join workspaces w on w.id = i.workspace_id
		where i.workspace_id::text = $1 and i.expires_at > now() order by i.created_at`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		i, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) InviteByToken(ctx context.Context, tokenHash string) (Invite, error) {
	return scanInvite(s.db.QueryRow(ctx, `
		select `+inviteColumns+` from invites i join workspaces w on w.id = i.workspace_id
		where i.token_hash = $1 and i.expires_at > now()`, tokenHash))
}

func (s *Store) DeleteInvite(ctx context.Context, ws, id string) error {
	_, err := s.db.Exec(ctx, `delete from invites where id::text = $1 and workspace_id::text = $2`, id, ws)
	return err
}

// takeInvite uses up an invite inside tx.
func takeInvite(ctx context.Context, tx pgx.Tx, tokenHash string) (email, role, ws string, err error) {
	err = tx.QueryRow(ctx, `delete from invites where token_hash = $1 and expires_at > now()
		returning email, role, workspace_id::text`, tokenHash).Scan(&email, &role, &ws)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// AcceptInvite turns an invite into a new account in its workspace, once.
func (s *Store) AcceptInvite(ctx context.Context, tokenHash, name, passwordHash string) (User, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	email, role, ws, err := takeInvite(ctx, tx, tokenHash)
	if err != nil {
		return User{}, err
	}
	u, err := scanUser(tx.QueryRow(ctx, `
		insert into users (email, name, password_hash, role) values ($1, $2, $3, 'member') returning `+userColumns,
		email, name, passwordHash))
	if isUnique(err) {
		return u, ErrExists
	} else if err != nil {
		return u, err
	}
	if _, err := tx.Exec(ctx, `insert into workspace_members (workspace_id, user_id, role) values ($1::uuid, $2, $3)`, ws, u.ID, role); err != nil {
		return u, err
	}
	return u, tx.Commit(ctx)
}

// JoinByInvite adds an existing account to the invite's workspace, once.
func (s *Store) JoinByInvite(ctx context.Context, tokenHash, userID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, role, ws, err := takeInvite(ctx, tx, tokenHash)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into workspace_members (workspace_id, user_id, role) values ($1::uuid, $2, $3)
		on conflict do nothing`, ws, userID, role); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// prefixed qualifies each column in a column list with a table alias.
func prefixed(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, c := range parts {
		parts[i] = alias + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

// CreatePasswordReset stores a reset link for an account, replacing any
// earlier one.
func (s *Store) CreatePasswordReset(ctx context.Context, userID, tokenHash string, expires time.Time) error {
	_, err := s.db.Exec(ctx, `insert into password_resets (token_hash, user_id, expires_at) values ($1, $2, $3)
		on conflict (user_id) do update set token_hash = excluded.token_hash, created_at = now(), expires_at = excluded.expires_at`,
		tokenHash, userID, expires)
	return err
}

// PasswordResetUser is the account a live reset link is for.
func (s *Store) PasswordResetUser(ctx context.Context, tokenHash string) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `select `+prefixed("u.", userColumns)+` from password_resets r
		join users u on u.id = r.user_id where r.token_hash = $1 and r.expires_at > now()`, tokenHash))
}

// ResetPassword uses a reset link once: it sets the account's password and
// signs out all its sessions.
func (s *Store) ResetPassword(ctx context.Context, tokenHash, passwordHash string) (User, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	var userID string
	err = tx.QueryRow(ctx, `delete from password_resets where token_hash = $1 and expires_at > now() returning user_id`, tokenHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	} else if err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(ctx, `update users set password_hash = $2 where id = $1`, userID, passwordHash); err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(ctx, `delete from sessions where user_id = $1`, userID); err != nil {
		return User{}, err
	}
	u, err := scanUser(tx.QueryRow(ctx, `select `+userColumns+` from users where id = $1`, userID))
	if err != nil {
		return User{}, err
	}
	return u, tx.Commit(ctx)
}

// DeleteExpiredPasswordResets clears links nobody used.
func (s *Store) DeleteExpiredPasswordResets(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `delete from password_resets where expires_at <= now()`)
	return err
}
