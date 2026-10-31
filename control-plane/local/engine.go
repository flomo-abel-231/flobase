package local

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FloBase needs a Docker engine. EnsureEngine uses the one on the computer
// when it runs, starts it when it is installed but stopped, and otherwise
// installs one (engine_darwin.go, engine_windows.go, engine_linux.go):
//
//   - macOS: Colima and Lima, which run a small Linux VM with Apple's
//     Virtualization framework, plus the Docker CLI and Compose, all in
//     ~/.flobase/runtime. No admin rights, no Docker Desktop licence.
//   - Windows: WSL 2 and Docker Desktop, through winget.
//   - Linux: Docker Engine, with Docker's install script.
//
// FLOBASE_ENGINE=bundled makes macOS use its own engine even when another
// one runs.

// ErrRestartRequired means an installed requirement only works after the
// user restarts or signs in again.
type ErrRestartRequired struct{ What string }

func (e *ErrRestartRequired) Error() string { return e.What }

// EnsureEngine makes sure a Docker engine with Compose v2 answers,
// installing and starting one if needed.
func (m *Manager) EnsureEngine(ctx context.Context) error {
	m.useOwnEngine()
	if (!bundledOnly() || m.ownEngine) && m.engineReady(ctx) {
		return nil
	}
	if err := m.startOrInstallEngine(ctx); err != nil {
		return err
	}
	if !m.engineReady(ctx) {
		return &ErrNoDocker{Detail: "the Docker engine did not answer after starting"}
	}
	return nil
}

func bundledOnly() bool { return os.Getenv("FLOBASE_ENGINE") == "bundled" }

// waitEngine waits for the engine to answer.
func (m *Manager) waitEngine(ctx context.Context, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if m.engineReady(ctx) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
	return false
}

// fetch downloads url to dest; with sum set, the file's SHA-256 must match.
func fetch(ctx context.Context, url, dest, sum string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if sum != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), sum) {
		os.Remove(tmp)
		return fmt.Errorf("download %s: checksum does not match", url)
	}
	return os.Rename(tmp, dest)
}

// fetchText downloads a small text file such as a checksum list.
func fetchText(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(b), err
}

// sumFor finds name's SHA-256 in a checksum list ("<hash>  name" or
// "<hash> *name" lines), or the single hash of a one-file .sha256.
func sumFor(list, name string) string {
	sc := bufio.NewScanner(strings.NewReader(list))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 1 && len(f[0]) == 64 {
			return f[0]
		}
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0]
		}
	}
	return ""
}

// untar extracts a .tar.gz into dir. keep, when set, picks which entries
// to extract and where (relative to dir); an empty result skips one.
func untar(archive, dir string, keep func(name string) string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		name := filepath.Clean(strings.TrimPrefix(h.Name, "./"))
		if keep != nil {
			name = keep(name)
		}
		if name == "" || name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		target := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode)&0o755|0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			if strings.HasPrefix(h.Linkname, "/") || strings.Contains(h.Linkname, "..") {
				continue
			}
			_ = os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
	}
}
