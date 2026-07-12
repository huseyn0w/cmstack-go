package media

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/storage"
)

// ErrNotFound is the sentinel the repository returns when a media row is absent.
// The service maps it to domain outcomes; handlers turn it into a 404.
var ErrNotFound = errors.New("media: not found")

// CreateMediaData is the fully-prepared row the repo inserts. The service has
// already validated the upload, derived the key, and probed dimensions.
type CreateMediaData struct {
	StorageKey       string
	OriginalFilename string
	MIME             string
	SizeBytes        int64
	Width            *int
	Height           *int
	Alt              string
	Title            string
	Caption          string
	UploadedBy       uuid.UUID
}

// CreateThumbnailData is one generated variant row.
type CreateThumbnailData struct {
	MediaID    uuid.UUID
	Variant    string
	StorageKey string
	Width      int
	Height     int
}

// Repository is the data-access contract for media. It is the ONLY layer
// permitted to touch sqlc/pgx for media. Create is transactional (the row and
// its thumbnail variants commit atomically); reads/deletes are simple ops.
type Repository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, in CreateMediaData) (Media, error)
	CreateThumbnailTx(ctx context.Context, tx pgx.Tx, in CreateThumbnailData) (Thumbnail, error)

	GetByID(ctx context.Context, id uuid.UUID) (Media, error)
	List(ctx context.Context, limit, offset int) ([]Media, error)
	Count(ctx context.Context) (int, error)

	UpdateMetadata(ctx context.Context, id uuid.UUID, alt, title, caption string) (Media, error)
	Delete(ctx context.Context, id uuid.UUID) error

	// ThumbnailsForMedia loads a single asset's variants (used by Delete to find
	// every storage object to remove).
	ThumbnailsForMedia(ctx context.Context, mediaID uuid.UUID) ([]Thumbnail, error)
}

// Authorizer answers (action, subject) permission questions for a user. The
// accounts.Authorizer satisfies it.
type Authorizer interface {
	Can(ctx context.Context, userID uuid.UUID, action, subject string) bool
}

// Publisher publishes a domain event inside a transaction. *events.Bus
// satisfies it.
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, event events.Event) error
}

// BlobStore is the subset of storage.Storage the media service needs. Declaring
// it locally keeps the dependency narrow and the service fakeable. *LocalStorage
// and *S3Storage both satisfy it.
type BlobStore interface {
	Save(ctx context.Context, key string, r io.Reader, contentType string) (string, error)
	Delete(ctx context.Context, key string) error
	URL(key string) string
}

// Validator validates an upload by magic bytes against the allow-list. The
// concrete *storage.Validator satisfies it. Declared as an interface so the
// service can be unit-tested with a fake that returns a canned result.
type Validator interface {
	Validate(r io.Reader) (storage.ValidatedMedia, error)
	MaxBytes() int64
}

// Thumbnailer generates raster variants from validated image bytes. The package
// function storage.GenerateThumbnails is adapted to this by the wiring; declared
// as an interface so the service can assert generation was invoked in tests.
type Thumbnailer interface {
	Generate(data []byte, sourceMIME string) ([]storage.GeneratedThumbnail, error)
}
