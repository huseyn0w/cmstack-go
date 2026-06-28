package kernel

import "github.com/google/uuid"

// BulkResult is the shared outcome of a bulk admin list action (M2c). A bulk
// operation iterates the submitted ids and reuses the existing single-item
// service method per id, so ownership/permission/sanitize/events all stay
// correct. A per-item authorization failure (an Author submitting another
// user's post id) does NOT abort the batch: the id is collected into
// SkippedUnauthorized and the loop continues. Ids whose row no longer exists
// land in NotFound. Applied carries the ids the action actually changed.
//
// The three slices are disjoint and together never exceed the submitted set
// (duplicate ids are de-duplicated by the caller before iterating).
type BulkResult struct {
	// Applied are the ids the action successfully changed.
	Applied []uuid.UUID
	// SkippedUnauthorized are ids the actor was not permitted to act on
	// (coarse permission OR per-item ownership). They are skipped, not failed.
	SkippedUnauthorized []uuid.UUID
	// NotFound are ids that did not resolve to a row.
	NotFound []uuid.UUID
}

// AppliedCount is the number of ids the action changed.
func (r BulkResult) AppliedCount() int { return len(r.Applied) }

// SkippedCount is the number of ids skipped as unauthorized.
func (r BulkResult) SkippedCount() int { return len(r.SkippedUnauthorized) }

// NotFoundCount is the number of submitted ids with no matching row.
func (r BulkResult) NotFoundCount() int { return len(r.NotFound) }

// markApplied records id as changed.
func (r *BulkResult) markApplied(id uuid.UUID) { r.Applied = append(r.Applied, id) }

// markUnauthorized records id as skipped for permission/ownership.
func (r *BulkResult) markUnauthorized(id uuid.UUID) {
	r.SkippedUnauthorized = append(r.SkippedUnauthorized, id)
}

// markNotFound records id as having no matching row.
func (r *BulkResult) markNotFound(id uuid.UUID) { r.NotFound = append(r.NotFound, id) }

// DedupeIDs returns ids with the zero UUID and duplicates removed, preserving
// first-seen order. The bulk driver de-duplicates so a doubly-submitted id is
// counted once.
func DedupeIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// RunBulk is the shared driver for a bulk action. For each (de-duplicated) id it
// invokes apply; classify maps a returned error to one of the skip buckets
// (unauthorized / not-found) and reports whether it was a recognized skip. A
// nil error counts as applied; an unrecognized error aborts the batch and is
// returned to the caller (a genuine infrastructure failure must not be silently
// swallowed). Recognized skips never abort: the loop continues.
func RunBulk(
	ids []uuid.UUID,
	apply func(id uuid.UUID) error,
	classify func(err error) (unauthorized bool, notFound bool, recognized bool),
) (BulkResult, error) {
	var res BulkResult
	for _, id := range DedupeIDs(ids) {
		err := apply(id)
		if err == nil {
			res.markApplied(id)
			continue
		}
		unauthorized, notFound, recognized := classify(err)
		switch {
		case recognized && unauthorized:
			res.markUnauthorized(id)
		case recognized && notFound:
			res.markNotFound(id)
		default:
			return res, err
		}
	}
	return res, nil
}
