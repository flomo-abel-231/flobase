package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"flobase/control-plane/internal/provisioner"
	"flobase/control-plane/internal/store"
)

// Storage locations: S3-compatible buckets for off-site backup copies,
// managed from the dashboard. At most one is active. A location set in
// .env (FLOBASE_BACKUP_S3_*) takes precedence and is shown read-only.

func (s *Server) storageRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/storages", s.platformAdmin(s.listStorages))
	mux.Handle("POST /api/v1/storages/test", s.platformAdmin(s.testStorage))
	mux.Handle("POST /api/v1/storages", s.platformAdmin(s.createStorage))
	mux.Handle("POST /api/v1/storages/{id}/activate", s.platformAdmin(s.activateStorage))
	mux.Handle("POST /api/v1/storages/deactivate", s.platformAdmin(s.deactivateStorage))
	mux.Handle("DELETE /api/v1/storages/{id}", s.platformAdmin(s.deleteStorage))
}

func toOffsite(l store.StorageLocation) *provisioner.Offsite {
	return &provisioner.Offsite{
		Endpoint: l.Endpoint, Region: l.Region, Bucket: l.Bucket, Prefix: l.Prefix,
		AccessKey: l.AccessKey, SecretKey: l.SecretKey, EncryptionKey: l.EncryptionKey,
	}
}

// ApplyStorage points off-site copies at the configured location (.env
// first, else the active one in the database) and restarts every
// project's sync container to match.
func (s *Server) ApplyStorage(ctx context.Context) {
	o := s.cfg.EnvOffsite
	if o == nil {
		if l, err := s.store.ActiveStorageLocation(ctx); err == nil {
			o = toOffsite(l)
		} else if !errors.Is(err, store.ErrNotFound) {
			s.log.Error("read storage locations", "err", err)
		}
	}
	s.prov.SetOffsite(o)
	if o != nil {
		s.log.Info("off-site backups on", "bucket", o.Bucket, "encrypted", o.EncryptionKey != "")
	}
	projects, err := s.store.Projects(ctx)
	if err != nil {
		return
	}
	for _, p := range projects {
		_ = s.prov.StopOffsite(ctx, p.Ref)
	}
	s.EnsureOffsiteAll(ctx)
}

// hint shows enough of a key to tell keys apart, never the whole key.
func hint(key string) string {
	if len(key) <= 4 {
		return "••••"
	}
	return "••••" + key[len(key)-4:]
}

type storageView struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Provider      string    `json:"provider"`
	Endpoint      string    `json:"endpoint"`
	Region        string    `json:"region"`
	Bucket        string    `json:"bucket"`
	Prefix        string    `json:"prefix"`
	AccessKeyHint string    `json:"access_key_hint"`
	Encrypted     bool      `json:"encrypted"`
	Active        bool      `json:"active"`
	FromEnv       bool      `json:"from_env"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
}

func (s *Server) listStorages(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.StorageLocations(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	views := make([]storageView, 0, len(list)+1)
	if o := s.cfg.EnvOffsite; o != nil {
		views = append(views, storageView{
			ID: "env", Name: "From .env", Provider: "custom", Endpoint: o.Endpoint, Region: o.Region,
			Bucket: o.Bucket, Prefix: o.Prefix, AccessKeyHint: hint(o.AccessKey),
			Encrypted: o.EncryptionKey != "", Active: true, FromEnv: true,
		})
	}
	for _, l := range list {
		views = append(views, storageView{
			ID: l.ID, Name: l.Name, Provider: l.Provider, Endpoint: l.Endpoint, Region: l.Region,
			Bucket: l.Bucket, Prefix: l.Prefix, AccessKeyHint: hint(l.AccessKey),
			Encrypted: l.EncryptionKey != "", Active: l.Active && s.cfg.EnvOffsite == nil, CreatedAt: l.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"locations": views, "env_managed": s.cfg.EnvOffsite != nil})
}

type storageRequest struct {
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	Endpoint      string `json:"endpoint"`
	Region        string `json:"region"`
	Bucket        string `json:"bucket"`
	Prefix        string `json:"prefix"`
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key"`
	EncryptionKey string `json:"encryption_key"`
	Activate      bool   `json:"activate"`
}

// location validates a request and turns it into a location.
func (req storageRequest) location() (store.StorageLocation, error) {
	l := store.StorageLocation{
		Name: strings.TrimSpace(req.Name), Provider: strings.TrimSpace(req.Provider),
		Endpoint: strings.TrimRight(strings.TrimSpace(req.Endpoint), "/"), Region: strings.TrimSpace(req.Region),
		Bucket: strings.TrimSpace(req.Bucket), Prefix: strings.Trim(strings.TrimSpace(req.Prefix), "/"),
		AccessKey: strings.TrimSpace(req.AccessKey), SecretKey: strings.TrimSpace(req.SecretKey),
		EncryptionKey: strings.TrimSpace(req.EncryptionKey),
	}
	if l.Provider == "" {
		l.Provider = "custom"
	}
	if l.Region == "" {
		l.Region = "auto"
	}
	u, err := url.Parse(l.Endpoint)
	switch {
	case utf8.RuneCountInString(l.Name) < 1 || utf8.RuneCountInString(l.Name) > 64:
		return l, errors.New("Give the location a name of up to 64 characters.")
	case err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "":
		return l, errors.New("The endpoint must be a URL such as https://s3.us-east-1.amazonaws.com.")
	case l.Bucket == "" || strings.ContainsAny(l.Bucket, "/ "):
		return l, errors.New("Give the bucket's name, without slashes.")
	case l.AccessKey == "" || l.SecretKey == "":
		return l, errors.New("The access key and secret key are both needed.")
	case l.EncryptionKey != "" && len(l.EncryptionKey) < 16:
		return l, errors.New("Use an encryption key of at least 16 characters, or leave it empty.")
	}
	return l, nil
}

// check tests a location, answering the request when it fails.
func (s *Server) check(w http.ResponseWriter, r *http.Request, l store.StorageLocation) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if err := s.prov.TestOffsite(ctx, toOffsite(l)); err != nil {
		writeError(w, http.StatusBadRequest, "FloBase could not write a test file to the bucket. Check the endpoint, region, bucket name and keys, and that the keys may write to the bucket. ("+rcloneReason(err.Error())+")")
		return false
	}
	return true
}

func (s *Server) testStorage(w http.ResponseWriter, r *http.Request) {
	var req storageRequest
	if !decode(w, r, &req) {
		return
	}
	l, err := req.location()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.check(w, r, l) {
		writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
	}
}

func (s *Server) createStorage(w http.ResponseWriter, r *http.Request) {
	var req storageRequest
	if !decode(w, r, &req) {
		return
	}
	l, err := req.location()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.check(w, r, l) {
		return
	}
	saved, err := s.store.CreateStorageLocation(r.Context(), l)
	if err != nil {
		s.internal(w, err)
		return
	}
	// The first location becomes active by itself; later ones on request.
	if s.cfg.EnvOffsite == nil {
		_, activeErr := s.store.ActiveStorageLocation(r.Context())
		if req.Activate || errors.Is(activeErr, store.ErrNotFound) {
			if err := s.store.ActivateStorageLocation(r.Context(), saved.ID); err != nil {
				s.internal(w, err)
				return
			}
			s.ApplyStorage(context.WithoutCancel(r.Context()))
		}
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": saved.ID})
}

func (s *Server) activateStorage(w http.ResponseWriter, r *http.Request) {
	if s.cfg.EnvOffsite != nil {
		writeError(w, http.StatusConflict, "Off-site storage is set in .env. Remove the FLOBASE_BACKUP_S3_ settings there to choose a location here.")
		return
	}
	if err := s.store.ActivateStorageLocation(r.Context(), r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Location not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	s.ApplyStorage(context.WithoutCancel(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deactivateStorage(w http.ResponseWriter, r *http.Request) {
	if s.cfg.EnvOffsite != nil {
		writeError(w, http.StatusConflict, "Off-site storage is set in .env; remove it there.")
		return
	}
	if err := s.store.ActivateStorageLocation(r.Context(), ""); err != nil {
		s.internal(w, err)
		return
	}
	s.ApplyStorage(context.WithoutCancel(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteStorage(w http.ResponseWriter, r *http.Request) {
	l, err := s.store.StorageLocation(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Location not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	if err := s.store.DeleteStorageLocation(r.Context(), l.ID); err != nil {
		s.internal(w, err)
		return
	}
	// Removing the location stops new copies; what is in the bucket stays.
	if l.Active {
		s.ApplyStorage(context.WithoutCancel(r.Context()))
	}
	w.WriteHeader(http.StatusNoContent)
}

// rcloneReason pulls rclone's own error out of its log output, without
// timestamps, for showing next to the plain explanation.
func rcloneReason(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "CRITICAL: "); i >= 0 {
			return strings.TrimSpace(line[i+len("CRITICAL: "):])
		}
		if i := strings.Index(line, "ERROR : "); i >= 0 {
			return strings.TrimSpace(line[i+len("ERROR : "):])
		}
	}
	if len(out) > 160 {
		out = out[:160] + "…"
	}
	return strings.TrimSpace(out)
}
