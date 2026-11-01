package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"flobase/control-plane/internal/catalog"
)

// RegionServer is the Docker host behind one region, with the credentials
// to drive it. The secrets stay on the server: the API never returns them.
type RegionServer struct {
	Region          catalog.Region
	Transport       string // ssh or tls
	Endpoint        string
	SSHKey          string
	SSHHostKey      string
	TLSCA           string
	TLSCert         string
	TLSKey          string
	PublicHost      string
	RelayPort       int
	RelayServerCert string
	RelayServerKey  string
	RelayClientCert string
	RelayClientKey  string
	CreatedAt       time.Time
}

const regionServerColumns = `r.id, r.name, r.provider, r.location, r.status, r.price_multiplier::float8,
	s.transport, s.endpoint, s.ssh_key, s.ssh_host_key, s.tls_ca, s.tls_cert, s.tls_key, s.public_host,
	s.relay_port, s.relay_server_cert, s.relay_server_key, s.relay_client_cert, s.relay_client_key, s.created_at`

func scanRegionServer(row pgx.Row) (RegionServer, error) {
	var v RegionServer
	r := &v.Region
	err := row.Scan(&r.ID, &r.Name, &r.Provider, &r.Location, &r.Status, &r.PriceMultiplier,
		&v.Transport, &v.Endpoint, &v.SSHKey, &v.SSHHostKey, &v.TLSCA, &v.TLSCert, &v.TLSKey, &v.PublicHost,
		&v.RelayPort, &v.RelayServerCert, &v.RelayServerKey, &v.RelayClientCert, &v.RelayClientKey, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

func (s *Store) RegionServers(ctx context.Context) ([]RegionServer, error) {
	rows, err := s.db.Query(ctx, `select `+regionServerColumns+`
		from region_servers s join regions r on r.id = s.region_id order by s.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RegionServer
	for rows.Next() {
		v, err := scanRegionServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) RegionServer(ctx context.Context, id string) (RegionServer, error) {
	return scanRegionServer(s.db.QueryRow(ctx, `select `+regionServerColumns+`
		from region_servers s join regions r on r.id = s.region_id where s.region_id = $1`, id))
}

// ErrRegionExists is returned when a region with the same id exists.
var ErrRegionExists = errors.New("region exists")

// CreateRegionServer adds the region and its server together.
func (s *Store) CreateRegionServer(ctx context.Context, v RegionServer) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	r := v.Region
	tag, err := tx.Exec(ctx, `
		insert into regions (id, name, provider, location, status, price_multiplier)
		values ($1, $2, 'docker', $3, $4, $5) on conflict (id) do nothing`,
		r.ID, r.Name, r.Location, r.Status, r.PriceMultiplier)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRegionExists
	}
	if _, err := tx.Exec(ctx, `
		insert into region_servers (region_id, transport, endpoint, ssh_key, ssh_host_key, tls_ca, tls_cert, tls_key,
			public_host, relay_port, relay_server_cert, relay_server_key, relay_client_cert, relay_client_key)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		r.ID, v.Transport, v.Endpoint, v.SSHKey, v.SSHHostKey, v.TLSCA, v.TLSCert, v.TLSKey,
		v.PublicHost, v.RelayPort, v.RelayServerCert, v.RelayServerKey, v.RelayClientCert, v.RelayClientKey); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateRegion changes what customers see of a region and whether it takes
// new projects.
func (s *Store) UpdateRegion(ctx context.Context, r catalog.Region) error {
	tag, err := s.db.Exec(ctx, `
		update regions set name = $2, location = $3, status = $4, price_multiplier = $5 where id = $1`,
		r.ID, r.Name, r.Location, r.Status, r.PriceMultiplier)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ErrRegionInUse is returned when deleting a region that still has projects.
var ErrRegionInUse = errors.New("region has projects")

// DeleteRegionServer removes a region server and its region, which must
// have no projects left.
func (s *Store) DeleteRegionServer(ctx context.Context, id string) error {
	var n int
	if err := s.db.QueryRow(ctx, `select count(*) from projects where region = $1`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrRegionInUse
	}
	tag, err := s.db.Exec(ctx, `
		delete from regions where id = $1 and exists (select 1 from region_servers where region_id = $1)`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// RegionProjects counts each region's projects.
func (s *Store) RegionProjects(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.Query(ctx, `select region, count(*) from projects group by region`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
