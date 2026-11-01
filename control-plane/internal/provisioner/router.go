package provisioner

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// LocalRegion is the region served by the Docker host the control plane
// runs on.
const LocalRegion = "local"

// Router is the Provisioner the API uses: it sends each call to the Docker
// driver of the project's region. The local driver is embedded, so calls
// that are not about one project (updates, off-site listings) go to it.
type Router struct {
	*Docker
	// regionOf looks up which region a project is in.
	regionOf func(ctx context.Context, ref string) (string, error)
	mu       sync.RWMutex
	servers  map[string]*Docker
}

func NewRouter(local *Docker, regionOf func(ctx context.Context, ref string) (string, error)) *Router {
	return &Router{Docker: local, regionOf: regionOf, servers: map[string]*Docker{}}
}

// SetServer adds or replaces a region server's driver; nil removes it.
func (r *Router) SetServer(region string, d *Docker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d == nil {
		delete(r.servers, region)
		return
	}
	d.SetOffsite(r.Docker.currentOffsite())
	r.servers[region] = d
}

// Server returns the driver of a region server.
func (r *Router) Server(region string) (*Docker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.servers[region]
	return d, ok
}

// Region returns the driver for a region.
func (r *Router) Region(region string) (*Docker, error) {
	if region == "" || region == LocalRegion {
		return r.Docker, nil
	}
	if d, ok := r.Server(region); ok {
		return d, nil
	}
	return nil, fmt.Errorf("region %s has no server configured", region)
}

func (r *Router) project(ctx context.Context, ref string) (*Docker, error) {
	region, err := r.regionOf(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("look up the project's region: %w", err)
	}
	return r.Region(region)
}

func (r *Router) Create(ctx context.Context, s Spec) (Endpoint, error) {
	d, err := r.Region(s.Region)
	if err != nil {
		return Endpoint{}, err
	}
	return d.Create(ctx, s)
}

func (r *Router) Delete(ctx context.Context, ref string) error {
	d, err := r.project(ctx, ref)
	if err != nil {
		return err
	}
	return d.Delete(ctx, ref)
}

func (r *Router) Addr(ref, service string, containerPort, publishedPort int) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d, err := r.project(ctx, ref)
	if err != nil {
		return "127.0.0.1", 0
	}
	return d.Addr(ref, service, containerPort, publishedPort)
}

func (r *Router) SetPassword(ctx context.Context, s Spec, old string) error {
	d, err := r.project(ctx, s.Ref)
	if err != nil {
		return err
	}
	return d.SetPassword(ctx, s, old)
}

func (r *Router) Recreate(ctx context.Context, s Spec) error {
	d, err := r.project(ctx, s.Ref)
	if err != nil {
		return err
	}
	return d.Recreate(ctx, s)
}

func (r *Router) Logs(ctx context.Context, ref, service string, lines int) (string, error) {
	d, err := r.project(ctx, ref)
	if err != nil {
		return "", err
	}
	return d.Logs(ctx, ref, service, lines)
}

func (r *Router) TakeBackup(ctx context.Context, ref string) (Backup, error) {
	d, err := r.project(ctx, ref)
	if err != nil {
		return Backup{}, err
	}
	return d.TakeBackup(ctx, ref)
}

func (r *Router) Backups(ctx context.Context, ref string) ([]Backup, error) {
	d, err := r.project(ctx, ref)
	if err != nil {
		return nil, err
	}
	return d.Backups(ctx, ref)
}

func (r *Router) DeleteBackups(ctx context.Context, ref string, names []string, oldestKept string) error {
	d, err := r.project(ctx, ref)
	if err != nil {
		return err
	}
	return d.DeleteBackups(ctx, ref, names, oldestKept)
}

func (r *Router) FlushArchive(ctx context.Context, ref string) error {
	d, err := r.project(ctx, ref)
	if err != nil {
		return err
	}
	return d.FlushArchive(ctx, ref)
}

// SetOffsite applies to every server: each project's sync container runs
// next to its database.
func (r *Router) SetOffsite(o *Offsite) {
	r.Docker.SetOffsite(o)
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, d := range r.servers {
		d.SetOffsite(o)
	}
}

func (r *Router) EnsureOffsite(ctx context.Context, ref string, meta map[string]any) error {
	d, err := r.project(ctx, ref)
	if err != nil {
		return err
	}
	return d.EnsureOffsite(ctx, ref, meta)
}

func (r *Router) OffsiteSyncNow(ctx context.Context, ref string) error {
	d, err := r.project(ctx, ref)
	if err != nil {
		return err
	}
	return d.OffsiteSyncNow(ctx, ref)
}

func (r *Router) StopOffsite(ctx context.Context, ref string) error {
	d, err := r.project(ctx, ref)
	if err != nil {
		return err
	}
	return d.StopOffsite(ctx, ref)
}
