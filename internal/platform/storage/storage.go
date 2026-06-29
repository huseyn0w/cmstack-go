// Package storage is the blob-storage abstraction for user uploads (avatars and
// the M4 media library). It exposes a small Storage interface (Strategy/Adapter)
// with two interchangeable backends — LocalStorage (the dev/single-node default,
// served over HTTP with a hardened sniff-proof handler) and S3Storage (S3 or any
// S3-compatible provider: MinIO, Cloudflare R2) — selected at startup via New.
//
// On top of the interface it provides the M4 media security core: magic-byte
// upload validation against an allow-list (jpg/png/gif/webp/pdf; SVG rejected),
// the stored extension derived from the VALIDATED MIME (anti-polyglot), a size
// cap, a decompression-bomb guard (header-probed pixel area), and server-side
// thumbnail/dimension generation for raster images.
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
