package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Workspace is one organization: its own members, projects, invites and
// assistant. Nothing is shared between workspaces.
type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role,omitempty"` // the reader's role in it
	CreatedAt time.Time `json:"created_at"`
}

// Member is a person in a workspace with their role there.
type Member struct {
	User
	WorkspaceRole string `json:"workspace_role"`
}

func scanWorkspaces(rows pgx.Rows) ([]Workspace, error) {
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.Role, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// UserWorkspaces lists the workspaces someone belongs to, oldest first.
func (s *Store) UserWorkspaces(ctx context.Context, userID string) ([]Workspace, error) {
	rows, err := s.db.Query(ctx, `
		select w.id, w.name, m.role, w.created_at from workspaces w
		join workspace_members m on m.workspace_id = w.id
		where m.user_id = $1 order by w.created_at`, userID)
	if err != nil {
		return nil, err
	}
	return scanWorkspaces(rows)
}

// AllWorkspaces lists every workspace, for the install's API token.
func (s *Store) AllWorkspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := s.db.Query(ctx, `select id, name, 'owner', created_at from workspaces order by created_at`)
	if err != nil {
		return nil, err
	}
	return scanWorkspaces(rows)
}

// MemberRole is someone's role in a workspace; ErrNotFound when they are
// not in it.
func (s *Store) MemberRole(ctx context.Context, ws, userID string) (string, error) {
	var role string
	err := s.db.QueryRow(ctx, `select role from workspace_members where workspace_id::text = $1 and user_id = $2`, ws, userID).
		Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

func (s *Store) WorkspaceExists(ctx context.Context, ws string) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `select exists (select 1 from workspaces where id::text = $1)`, ws).Scan(&ok)
	return ok, err
}

func createWorkspace(ctx context.Context, tx pgx.Tx, name, userID string) (Workspace, error) {
	w := Workspace{Name: name, Role: "owner"}
	if err := tx.QueryRow(ctx, `insert into workspaces (name) values ($1) returning id, created_at`, name).
		Scan(&w.ID, &w.CreatedAt); err != nil {
		return w, err
	}
	_, err := tx.Exec(ctx, `insert into workspace_members (workspace_id, user_id, role) values ($1, $2, 'owner')`, w.ID, userID)
	return w, err
}

// ownerWorkspace gives the platform owner their first workspace: the one
// projects from before workspaces were moved into, if it has nobody in it
// yet, else a new one.
func ownerWorkspace(ctx context.Context, tx pgx.Tx, name, userID string) (Workspace, error) {
	w := Workspace{Role: "owner"}
	err := tx.QueryRow(ctx, `select id::text, name, created_at from workspaces w
		where not exists (select 1 from workspace_members m where m.workspace_id = w.id)
		order by created_at limit 1`).Scan(&w.ID, &w.Name, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return createWorkspace(ctx, tx, name, userID)
	} else if err != nil {
		return w, err
	}
	// It takes the name the owner gave.
	if _, err := tx.Exec(ctx, `update workspaces set name = $2 where id::text = $1`, w.ID, name); err != nil {
		return w, err
	}
	w.Name = name
	_, err = tx.Exec(ctx, `insert into workspace_members (workspace_id, user_id, role) values ($1::uuid, $2, 'owner')`, w.ID, userID)
	return w, err
}

// CreateWorkspace makes a workspace with userID as its owner.
func (s *Store) CreateWorkspace(ctx context.Context, name, userID string) (Workspace, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Workspace{}, err
	}
	defer tx.Rollback(ctx)
	w, err := createWorkspace(ctx, tx, name, userID)
	if err != nil {
		return w, err
	}
	return w, tx.Commit(ctx)
}

func (s *Store) RenameWorkspace(ctx context.Context, ws, name string) error {
	_, err := s.db.Exec(ctx, `update workspaces set name = $2 where id::text = $1`, ws, name)
	return err
}

// ErrWorkspaceInUse is returned when deleting a workspace that has projects.
var ErrWorkspaceInUse = errors.New("workspace has projects")

// DeleteWorkspace removes a workspace with no projects, and with it its
// members, invites, models, chats and automations.
func (s *Store) DeleteWorkspace(ctx context.Context, ws string) error {
	var n int
	if err := s.db.QueryRow(ctx, `select count(*) from projects where workspace_id::text = $1`, ws).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrWorkspaceInUse
	}
	_, err := s.db.Exec(ctx, `delete from workspaces where id::text = $1`, ws)
	return err
}

// SignUp creates an account and its first workspace together. platformRole
// is "owner" only for the person who runs this FloBase.
func (s *Store) SignUp(ctx context.Context, email, name, hash, platformRole, workspace string) (User, Workspace, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, Workspace{}, err
	}
	defer tx.Rollback(ctx)
	u, err := scanUser(tx.QueryRow(ctx, `
		insert into users (email, name, password_hash, role) values ($1, $2, $3, $4) returning `+userColumns,
		email, name, hash, platformRole))
	if isUnique(err) {
		return u, Workspace{}, ErrExists
	} else if err != nil {
		return u, Workspace{}, err
	}
	var w Workspace
	if platformRole == "owner" {
		w, err = ownerWorkspace(ctx, tx, workspace, u.ID)
	} else {
		w, err = createWorkspace(ctx, tx, workspace, u.ID)
	}
	if err != nil {
		return u, w, err
	}
	return u, w, tx.Commit(ctx)
}

func (s *Store) Members(ctx context.Context, ws string) ([]Member, error) {
	rows, err := s.db.Query(ctx, `select `+prefixed("u.", userColumns)+`, m.role from users u
		join workspace_members m on m.user_id = u.id where m.workspace_id::text = $1 order by m.created_at`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		u := &m.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.LastSignInAt, &m.WorkspaceRole); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMemberRole changes an admin or member; the workspace owner keeps
// their role.
func (s *Store) SetMemberRole(ctx context.Context, ws, userID, role string) error {
	tag, err := s.db.Exec(ctx, `update workspace_members set role = $3
		where workspace_id::text = $1 and user_id::text = $2 and role <> 'owner'`, ws, userID, role)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// RemoveMember takes an admin or member out of a workspace. Their account
// stays, with any other workspaces it belongs to.
func (s *Store) RemoveMember(ctx context.Context, ws, userID string) error {
	tag, err := s.db.Exec(ctx, `delete from workspace_members
		where workspace_id::text = $1 and user_id::text = $2 and role <> 'owner'`, ws, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// WorkspaceStorage is a workspace as the platform's owner manages it.
type WorkspaceStorage struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	OwnerEmail string    `json:"owner_email"`
	LimitMB    *int      `json:"limit_mb"`
	CreatedAt  time.Time `json:"created_at"`
}

// StorageLimit is the most storage a workspace's projects may take, in MB;
// nil = no limit.
func (s *Store) StorageLimit(ctx context.Context, ws string) (*int, error) {
	var limit *int
	err := s.db.QueryRow(ctx, `select storage_limit_mb from workspaces where id::text = $1`, ws).Scan(&limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return limit, err
}

func (s *Store) SetStorageLimit(ctx context.Context, ws string, limitMB *int) error {
	tag, err := s.db.Exec(ctx, `update workspaces set storage_limit_mb = $2 where id::text = $1`, ws, limitMB)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// WorkspacesStorage lists every workspace with its owner and limit.
func (s *Store) WorkspacesStorage(ctx context.Context) ([]WorkspaceStorage, error) {
	rows, err := s.db.Query(ctx, `
		select w.id, w.name, coalesce((select u.email from workspace_members m join users u on u.id = m.user_id
			where m.workspace_id = w.id and m.role = 'owner' order by m.created_at limit 1), ''),
			w.storage_limit_mb, w.created_at
		from workspaces w order by w.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceStorage{}
	for rows.Next() {
		var w WorkspaceStorage
		if err := rows.Scan(&w.ID, &w.Name, &w.OwnerEmail, &w.LimitMB, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
