package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LocalStorage writes objects to a directory tree under a base dir and serves
// them over HTTP at PublicPrefix. It is the dev/single-node backend; the S3
// driver arrives in M4 behind the same Storage interface.
type LocalStorage struct {
	baseDir string // filesystem root, e.g. ./uploads
	// publicPrefix is the URL path prefix the Handler is mounted at, e.g.
	// "/uploads". URL(key) joins it with the key.
	publicPrefix string
}

var _ Storage = (*LocalStorage)(nil)

// NewLocalStorage constructs a LocalStorage rooted at baseDir, serving at
// publicPrefix (default "/uploads" when empty). The base dir is created if
// missing.
func NewLocalStorage(baseDir, publicPrefix string) (*LocalStorage, error) {
	if baseDir == "" {
		return nil, errors.New("storage: empty upload dir")
	}
	if publicPrefix == "" {
		publicPrefix = "/uploads"
	}
	publicPrefix = "/" + strings.Trim(publicPrefix, "/")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("storage: create upload dir: %w", err)
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve upload dir: %w", err)
	}
	return &LocalStorage{baseDir: abs, publicPrefix: publicPrefix}, nil
}

// safePath resolves key to an absolute path INSIDE baseDir, rejecting any key
// that would traverse out (e.g. "../"). It is the single chokepoint guarding the
// filesystem against path-traversal regardless of how a key was built.
func (s *LocalStorage) safePath(key string) (string, error) {
	clean := path.Clean("/" + key) // collapse .. and leading slashes
	full := filepath.Join(s.baseDir, filepath.FromSlash(clean))
	if full != s.baseDir && !strings.HasPrefix(full, s.baseDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage: key escapes base dir: %q", key)
	}
	return full, nil
}

// Save streams r to baseDir/key, creating parent directories. contentType is
// accepted for interface parity (the local backend infers type at serve time);
// it is not persisted as a sidecar in this minimal implementation.
func (s *LocalStorage) Save(_ context.Context, key string, r io.Reader, _ string) (string, error) {
	full, err := s.safePath(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", fmt.Errorf("storage: mkdir: %w", err)
	}
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("storage: create file: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("storage: write file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("storage: close file: %w", err)
	}
	return key, nil
}

// Delete removes baseDir/key. A missing object is not an error.
func (s *LocalStorage) Delete(_ context.Context, key string) error {
	if key == "" {
		return nil
	}
	full, err := s.safePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storage: delete file: %w", err)
	}
	return nil
}

// URL returns the public URL for key.
func (s *LocalStorage) URL(key string) string {
	if key == "" {
		return ""
	}
	return s.publicPrefix + "/" + strings.TrimLeft(key, "/")
}

// PublicPrefix is the URL prefix the Handler must be mounted at.
func (s *LocalStorage) PublicPrefix() string { return s.publicPrefix }

// Handler serves stored objects read-only with hardened headers. It sets
// X-Content-Type-Options: nosniff so a browser never re-interprets a stored blob
// as a different (e.g. active) type, and a conservative Content-Security-Policy
// + Content-Disposition so any HTML/SVG that slipped through cannot execute in
// our origin. Path traversal is blocked by safePath.
func (s *LocalStorage) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, s.publicPrefix+"/")
		full, err := s.safePath(key)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		// PDFs can embed JS; serve them as a download (belt-and-suspenders on top
		// of the sandbox CSP) rather than rendering inline. Images stay inline.
		if strings.HasSuffix(strings.ToLower(full), ".pdf") {
			w.Header().Set("Content-Disposition", "attachment")
		} else {
			w.Header().Set("Content-Disposition", "inline")
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeFile(w, r, full)
	})
}
