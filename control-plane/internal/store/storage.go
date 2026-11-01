package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// StorageLocation is an S3-compatible bucket off-site backup copies can go
// to. The secrets stay on the server: the API never returns them.
type StorageLocation struct {
	ID            string
	Name          string
	Provider      string
	Endpoint      string
	Region        string
	Bucket        string
	Prefix        string
	AccessKey     string
	SecretKey     string
	EncryptionKey string
	Active        bool
	CreatedAt     time.Time
}

const storageColumns = `id, name, provider, endpoint, region, bucket, prefix, access_key, secret_key, encryption_key, active, created_at`

func scanStorage(row pgx.Row) (StorageLocation, error) {
	var l StorageLocation
	err := row.Scan(&l.ID, &l.Name, &l.Provider, &l.Endpoint, &l.Region, &l.Bucket, &l.Prefix,
		&l.AccessKey, &l.SecretKey, &l.EncryptionKey, &l.Active, &l.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

func (s *Store) StorageLocations(ctx context.Context) ([]StorageLocation, error) {
	rows, err := s.db.Query(ctx, `select `+storageColumns+` from storage_locations order by created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StorageLocation
	for rows.Next() {
		l, err := scanStorage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) StorageLocation(ctx context.Context, id string) (StorageLocation, error) {
	return scanStorage(s.db.QueryRow(ctx, `select `+storageColumns+` from storage_locations where id::text = $1`, id))
}

// ActiveStorageLocation is the location copies go to; ErrNotFound when none.
func (s *Store) ActiveStorageLocation(ctx context.Context) (StorageLocation, error) {
	return scanStorage(s.db.QueryRow(ctx, `select `+storageColumns+` from storage_locations where active`))
}

func (s *Store) CreateStorageLocation(ctx context.Context, l StorageLocation) (StorageLocation, error) {
	return scanStorage(s.db.QueryRow(ctx, `
		insert into storage_locations (name, provider, endpoint, region, bucket, prefix, access_key, secret_key, encryption_key)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		returning `+storageColumns,
		l.Name, l.Provider, l.Endpoint, l.Region, l.Bucket, l.Prefix, l.AccessKey, l.SecretKey, l.EncryptionKey))
}

// ActivateStorageLocation makes id the only active location; an empty id
// makes none active.
func (s *Store) ActivateStorageLocation(ctx context.Context, id string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `update storage_locations set active = false where active`); err != nil {
		return err
	}
	if id != "" {
		tag, err := tx.Exec(ctx, `update storage_locations set active = true where id::text = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteStorageLocation(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, `delete from storage_locations where id::text = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
