package kernel

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Entity-type discriminators for the shared revisions table. Each content type
// that wants revision history registers its own constant here.
const (
	// EntityTypePost is the revision entity_type for posts.
	EntityTypePost = "post"
	// EntityTypePage is reserved for the next milestone's pages.
	EntityTypePage = "page"
)

// Revision is one immutable snapshot of a content entity captured BEFORE an
// update. Snapshot is opaque JSON owned by the calling domain (e.g. the post's
// scalar fields); the kernel never interprets it beyond storing and returning
// it.
type Revision struct {
	ID         uuid.UUID
	EntityType string
	EntityID   uuid.UUID
	Snapshot   json.RawMessage
	AuthorID   *uuid.UUID // nil for system-authored snapshots
	CreatedAt  time.Time
}

// CreateRevisionInput is the data needed to persist a new snapshot.
type CreateRevisionInput struct {
	EntityType string
	EntityID   uuid.UUID
	Snapshot   json.RawMessage
	AuthorID   *uuid.UUID
}

// RevisionRepository is the data-access contract for the shared revisions table.
// It is the ONLY layer permitted to touch sqlc/pgx for revisions; the kernel
// service and the owning domain depend solely on this interface. CreateTx is
// transactional so a snapshot can be captured in the SAME transaction as the
// update that supersedes it (the revision is a SYNC, in-tx side effect).
type RevisionRepository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, in CreateRevisionInput) (Revision, error)
	List(ctx context.Context, entityType string, entityID uuid.UUID) ([]Revision, error)
	Get(ctx context.Context, id uuid.UUID) (Revision, error)
}

// MarshalSnapshot is a small helper so callers building a snapshot do not repeat
// the json.Marshal boilerplate. Any value that marshals to JSON is accepted.
func MarshalSnapshot(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}
