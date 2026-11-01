// Package store keeps the control plane's own state in Postgres: regions and
// projects. It records what each project should look like; the provisioner
// makes the infrastructure match.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"flobase/control-plane/internal/catalog"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrNotFound = errors.New("not found")

const (
	StatusProvisioning = "provisioning"
	StatusActive       = "active"
	StatusFailed       = "failed"
	StatusDeleting     = "deleting"
)

type Project struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	Ref          string    `json:"ref"`
	Name         string    `json:"name"`
	Engine       string    `json:"engine"`
	Region       string    `json:"region"`
	Plan         string    `json:"plan"`
	Status       string    `json:"status"`
	StatusDetail string    `json:"status_detail"`
	DBHost       string    `json:"-"`
	DBPort       int       `json:"-"`
	PoolerPort   int       `json:"-"`
	DBUser       string    `json:"-"`
	DBPassword   string    `json:"-"`
	DBName       string    `json:"-"`
	JWTSecret    string    `json:"-"`
	AnonKey      string    `json:"-"`
	ServiceKey   string    `json:"-"`
	TLS          bool      `json:"tls"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Store struct{ db *pgxpool.Pool }

// Open connects, retrying while the database starts, then applies migrations.
func Open(ctx context.Context, url string) (*Store, error) {
	s, err := Connect(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := s.migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// Connect connects without migrating, for readers such as the gateway.
func Connect(ctx context.Context, url string) (*Store, error) {
	var pool *pgxpool.Pool
	var err error
	for attempt := 0; attempt < 30; attempt++ {
		pool, err = pgxpool.New(ctx, url)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				break
			}
			pool.Close()
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to platform database: %w", err)
	}
	return &Store{db: pool}, nil
}

func (s *Store) Close() { s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		sql, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

// SeedRegions upserts the region catalogue. Status is only set on insert so
// an operator's open/closed choice survives restarts.
func (s *Store) SeedRegions(ctx context.Context, regions []catalog.Region) error {
	for _, r := range regions {
		_, err := s.db.Exec(ctx, `
			insert into regions (id, name, provider, location, status, price_multiplier)
			values ($1, $2, $3, $4, $5, $6)
			on conflict (id) do update set name = excluded.name, provider = excluded.provider,
				location = excluded.location, price_multiplier = excluded.price_multiplier`,
			r.ID, r.Name, r.Provider, r.Location, r.Status, r.PriceMultiplier)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Regions(ctx context.Context, providers []string) ([]catalog.Region, error) {
	rows, err := s.db.Query(ctx, `
		select id, name, provider, location, status, price_multiplier::float8
		from regions where provider = any($1) order by provider = 'docker' desc, provider, name`, providers)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (catalog.Region, error) {
		var r catalog.Region
		err := row.Scan(&r.ID, &r.Name, &r.Provider, &r.Location, &r.Status, &r.PriceMultiplier)
		return r, err
	})
}

func (s *Store) Region(ctx context.Context, id string) (catalog.Region, error) {
	var r catalog.Region
	err := s.db.QueryRow(ctx, `
		select id, name, provider, location, status, price_multiplier::float8 from regions where id = $1`, id).
		Scan(&r.ID, &r.Name, &r.Provider, &r.Location, &r.Status, &r.PriceMultiplier)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

const projectColumns = `id, coalesce(workspace_id::text, ''), ref, name, engine, region, plan, status, status_detail, db_host, db_port, pooler_port,
	db_user, db_password, db_name, jwt_secret, anon_key, service_key, tls, created_at, updated_at`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.Ref, &p.Name, &p.Engine, &p.Region, &p.Plan, &p.Status, &p.StatusDetail,
		&p.DBHost, &p.DBPort, &p.PoolerPort, &p.DBUser, &p.DBPassword, &p.DBName, &p.JWTSecret, &p.AnonKey, &p.ServiceKey,
		&p.TLS, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) CreateProject(ctx context.Context, p Project) (Project, error) {
	return scanProject(s.db.QueryRow(ctx, `
		insert into projects (workspace_id, ref, name, engine, region, plan, status, db_user, db_password, db_name,
			jwt_secret, anon_key, service_key)
		values ($13::uuid, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		returning `+projectColumns,
		p.Ref, p.Name, p.Engine, p.Region, p.Plan, StatusProvisioning, p.DBUser, p.DBPassword, p.DBName,
		p.JWTSecret, p.AnonKey, p.ServiceKey, p.WorkspaceID))
}

func (s *Store) Projects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.Query(ctx, `select `+projectColumns+` from projects order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// WorkspaceProjects lists one workspace's projects.
func (s *Store) WorkspaceProjects(ctx context.Context, ws string) ([]Project, error) {
	rows, err := s.db.Query(ctx, `select `+projectColumns+` from projects where workspace_id::text = $1 order by created_at desc`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Project(ctx context.Context, ref string) (Project, error) {
	return scanProject(s.db.QueryRow(ctx, `select `+projectColumns+` from projects where ref = $1`, ref))
}

func (s *Store) SetStatus(ctx context.Context, ref, status, detail string) error {
	_, err := s.db.Exec(ctx, `update projects set status = $2, status_detail = $3, updated_at = now() where ref = $1`,
		ref, status, detail)
	return err
}

func (s *Store) SetActive(ctx context.Context, ref, host string, port, poolerPort int) error {
	_, err := s.db.Exec(ctx, `
		update projects set status = $2, status_detail = '', db_host = $3, db_port = $4, pooler_port = $5,
			updated_at = now()
		where ref = $1`, ref, StatusActive, host, port, poolerPort)
	return err
}

// SetTLS records whether a project's database serves TLS.
func (s *Store) SetTLS(ctx context.Context, ref string, on bool) error {
	_, err := s.db.Exec(ctx, `update projects set tls = $2, updated_at = now() where ref = $1`, ref, on)
	return err
}

func (s *Store) SetPassword(ctx context.Context, ref, password string) error {
	_, err := s.db.Exec(ctx, `update projects set db_password = $2, updated_at = now() where ref = $1`, ref, password)
	return err
}

func (s *Store) DeleteProject(ctx context.Context, ref string) error {
	_, err := s.db.Exec(ctx, `delete from projects where ref = $1`, ref)
	return err
}

// FailInterrupted marks projects left mid-provisioning by a restart.
func (s *Store) FailInterrupted(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `
		update projects set status = $1, status_detail = 'Interrupted by a control-plane restart. Delete and recreate it.',
			updated_at = now()
		where status = $2`, StatusFailed, StatusProvisioning)
	return err
}

// Deleting returns projects whose deletion did not finish, to resume it.
func (s *Store) Deleting(ctx context.Context) ([]Project, error) {
	all, err := s.Projects(ctx)
	if err != nil {
		return nil, err
	}
	var out []Project
	for _, p := range all {
		if p.Status == StatusDeleting {
			out = append(out, p)
		}
	}
	return out, nil
}
