// Package storage is a minimal blob-storage abstraction for user uploads
// (avatars now; richer media later). It exposes a small Storage interface plus a
// LocalStorage implementation that writes under an upload directory and serves
// the files over HTTP with a hardened, sniff-proof handler.
//
// NOTE(M4): this is intentionally small. The full media driver — S3/object
// storage backends, thumbnail/variant generation, content-addressing, and a
// media library UI — lands in M4 and will extend (not replace) this interface.
package storage

import (
	"context"
	"io"
)

// Storage persists opaque blobs under a caller-chosen key and resolves a public
// URL for them. Implementations MUST treat key as an already-sanitized,
// slash-delimited path (the caller derives it; see ObjectKey).
type Storage interface {
	// Save streams r to the object identified by key, recording contentType, and
	// returns the key actually stored under (callers persist this). It overwrites
	// any existing object at key.
	Save(ctx context.Context, key string, r io.Reader, contentType string) (string, error)
	// Delete removes the object at key. Deleting a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// URL returns the public URL at which key is served.
	URL(key string) string
}
