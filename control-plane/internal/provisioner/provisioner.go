// Package provisioner turns a project record into a running database and
// its services. The Docker driver serves self-hosting and the local dev
// region, and each cloud region is a Docker host of its own (a region
// server) driven remotely; Router sends each call to the project's host.
package provisioner

import (
	"context"
	"time"

	"flobase/control-plane/internal/catalog"
)

// Spec is everything a driver needs to create one project.
type Spec struct {
	Ref string
	// Region picks the Docker host (see Router); empty = the local one.
	Region    string
	Engine    string
	Plan      catalog.Plan
	User      string
	Password  string
	Database  string
	JWTSecret string
	// AuthURL is the public URL of the project's auth API, used by the auth
	// service in links and as the JWT issuer.
	AuthURL string
	// Restore, when set, fills the new database from another project's
	// backups instead of starting empty.
	Restore *Restore
}

// Restore picks the source and the moment a new project starts from.
type Restore struct {
	FromRef string
	// Backup is the base backup replay starts from; it must have finished
	// before Target.
	Backup string
	// Target is when replay stops; zero = as late as the archive goes.
	// Immediate stops at the backup itself.
	Target    time.Time
	Immediate bool
	// Offsite reads the source from off-site storage instead of its volumes
	// on this machine, so the source project need not exist here.
	Offsite bool
}

// Backup is one full backup of a project.
type Backup struct {
	Name      string    `json:"name"`
	Finished  time.Time `json:"finished"`
	SizeBytes int64     `json:"size_bytes"`
}

// Endpoint is where clients connect once the project is ready.
type Endpoint struct {
	Host       string
	Port       int
	PoolerPort int // 0 when the engine has no pooler
}

type Provisioner interface {
	// Create starts the database and its services and blocks until they are
	// healthy.
	Create(ctx context.Context, s Spec) (Endpoint, error)
	// Delete removes the project's containers and data. Deleting a project
	// that is already gone is not an error.
	Delete(ctx context.Context, ref string) error
	// Addr is where the control plane reaches one of a project's services:
	// the service's name on the project network when the control plane is
	// attached to it, a local forward through the relay on a region server,
	// else the published host port.
	Addr(ref, service string, containerPort, publishedPort int) (string, int)
	// SetPassword gives the database users that have the project's
	// password a new one (s.Password), logging in with old.
	SetPassword(ctx context.Context, s Spec, old string) error
	// Recreate replaces the project's containers with ones built from s,
	// keeping its data and published ports.
	Recreate(ctx context.Context, s Spec) error
	// Logs returns the last lines a project service wrote.
	Logs(ctx context.Context, ref, service string, lines int) (string, error)

	// Backups (Postgres projects). Every change is archived as it happens;
	// full backups are the starting points restores replay from.
	TakeBackup(ctx context.Context, ref string) (Backup, error)
	// Backups lists finished backups, oldest first.
	Backups(ctx context.Context, ref string) ([]Backup, error)
	// DeleteBackups removes the named backups and the archived changes no
	// remaining backup needs.
	DeleteBackups(ctx context.Context, ref string, names []string, oldestKept string) error
	// FlushArchive archives the change log in progress, so a restore can
	// reach up to now.
	FlushArchive(ctx context.Context, ref string) error

	// Off-site copies (see offsite.go).
	OffsiteEnabled() bool
	OffsiteInfo() (bucket string, encrypted, enabled bool)
	SetOffsite(o *Offsite)
	TestOffsite(ctx context.Context, o *Offsite) error
	EnsureOffsite(ctx context.Context, ref string, meta map[string]any) error
	OffsiteSyncNow(ctx context.Context, ref string) error
	StopOffsite(ctx context.Context, ref string) error
	OffsiteProjects(ctx context.Context) ([]OffsiteProject, error)
}
