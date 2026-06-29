package storage

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Driver names a selectable storage backend (the STORAGE_DRIVER config value).
const (
	DriverLocal = "local"
	DriverS3    = "s3"
)

// DriverConfig is the backend-agnostic input to New. Driver selects the backend;
// the Local* fields configure LocalStorage and the S3 field configures S3Storage.
type DriverConfig struct {
	Driver string // "local" (default) | "s3"

	// Local backend.
	LocalBaseDir      string // filesystem root (e.g. ./uploads)
	LocalPublicPrefix string // URL prefix (e.g. /uploads)

	// S3 backend.
	S3 S3Config
}

// New constructs the configured Storage backend. Local is the default and the
// tested-by-default path; "s3" builds the S3 driver. An unknown driver is an
// error (fail fast at startup rather than silently falling back). It also returns
// the public prefix and an optional local file Handler: for the local backend
// the handler serves /uploads; for S3 the handler is nil (objects are served by
// S3/CDN directly) and the prefix is empty.
func New(ctx context.Context, cfg DriverConfig) (Storage, http.Handler, string, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "", DriverLocal:
		local, err := NewLocalStorage(cfg.LocalBaseDir, cfg.LocalPublicPrefix)
		if err != nil {
			return nil, nil, "", err
		}
		return local, local.Handler(), local.PublicPrefix(), nil
	case DriverS3:
		s3s, err := NewS3Storage(ctx, cfg.S3)
		if err != nil {
			return nil, nil, "", err
		}
		// S3 objects are served by S3/CDN, not by an app handler.
		return s3s, nil, "", nil
	default:
		return nil, nil, "", fmt.Errorf("storage: unknown driver %q (want local|s3)", cfg.Driver)
	}
}
