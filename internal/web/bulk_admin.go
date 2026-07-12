package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// bulkActor is the subset of a content service the bulk dispatcher needs. Each
// content service (*posts.Service, *pages.Service, *services.Manager) satisfies
// it directly because their Bulk* methods all share this signature and return
// the shared kernel.BulkResult (re-exported per package as BulkResult, an alias).
type bulkActor interface {
	BulkTrash(ctx context.Context, actor uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkRestore(ctx context.Context, actor uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkPermanentDelete(ctx context.Context, actor uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkPublish(ctx context.Context, actor uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkUnpublish(ctx context.Context, actor uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
}

// bulkAction is one allow-listed bulk verb. Unknown actions are rejected before
// any service call (a tampered form must never reach the service with an
// unrecognized verb).
type bulkAction string

const (
	bulkTrash     bulkAction = "trash"
	bulkRestore   bulkAction = "restore"
	bulkDelete    bulkAction = "delete"
	bulkPublish   bulkAction = "publish"
	bulkUnpublish bulkAction = "unpublish"
)

// bulkAllowed is the allow-list of accepted actions. A POST whose action is not
// a key here is rejected with 400 (Bad Request) before touching the service.
var bulkAllowed = map[bulkAction]struct{}{
	bulkTrash:     {},
	bulkRestore:   {},
	bulkDelete:    {},
	bulkPublish:   {},
	bulkUnpublish: {},
}

// handleBulk is the shared thin bulk endpoint. It parses action + ids[] from the
// form, validates the action against the allow-list, dispatches to the matching
// service Bulk* method (which enforces the SAME per-id permission + ownership
// checks as the single-item ops — unauthorized ids are skipped, not failed),
// then redirects back to redirectTo with a summary query the list surfaces via
// aria-live. The route-level RequirePermission already gates the coarse grant;
// per-id ownership refinement happens inside the service.
func handleBulk(w http.ResponseWriter, r *http.Request, svc bulkActor, redirectTo string) {
	u, _ := UserFromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	action := bulkAction(r.PostFormValue("action"))
	if _, ok := bulkAllowed[action]; !ok {
		http.Error(w, "unknown bulk action", http.StatusBadRequest)
		return
	}

	ids := parseBulkIDs(r.PostForm["ids"])
	if len(ids) == 0 {
		// Nothing selected — bounce back without calling the service.
		http.Redirect(w, r, redirectTo, http.StatusSeeOther)
		return
	}

	var (
		res kernel.BulkResult
		err error
	)
	switch action {
	case bulkTrash:
		res, err = svc.BulkTrash(r.Context(), u.ID, ids)
	case bulkRestore:
		res, err = svc.BulkRestore(r.Context(), u.ID, ids)
	case bulkDelete:
		res, err = svc.BulkPermanentDelete(r.Context(), u.ID, ids)
	case bulkPublish:
		res, err = svc.BulkPublish(r.Context(), u.ID, ids)
	case bulkUnpublish:
		res, err = svc.BulkUnpublish(r.Context(), u.ID, ids)
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, redirectTo+"?"+bulkSummaryQuery(action, res), http.StatusSeeOther)
}

// parseBulkIDs decodes the submitted ids[] values into UUIDs, dropping any that
// do not parse (a tampered/garbage id is silently ignored rather than failing
// the whole batch). The service de-duplicates again defensively.
func parseBulkIDs(raw []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	return out
}

// bulkSummaryQuery encodes the bulk outcome as a redirect query the list reads
// to render the aria-live summary banner.
func bulkSummaryQuery(action bulkAction, res kernel.BulkResult) string {
	q := url.Values{}
	q.Set("bulk", string(action))
	q.Set("applied", strconv.Itoa(res.AppliedCount()))
	if res.SkippedCount() > 0 {
		q.Set("skipped", strconv.Itoa(res.SkippedCount()))
	}
	if res.NotFoundCount() > 0 {
		q.Set("missing", strconv.Itoa(res.NotFoundCount()))
	}
	return q.Encode()
}

// bulkSummaryFromQuery reads the redirect summary query back into the view-model
// banner text. It returns the zero value when no bulk summary is present.
func bulkSummaryFromQuery(r *http.Request) webtempl.BulkSummary {
	q := r.URL.Query()
	action := q.Get("bulk")
	if action == "" {
		return webtempl.BulkSummary{}
	}
	applied, _ := strconv.Atoi(q.Get("applied"))
	skipped, _ := strconv.Atoi(q.Get("skipped"))
	missing, _ := strconv.Atoi(q.Get("missing"))
	return webtempl.BulkSummary{
		Present: true,
		Action:  action,
		Applied: applied,
		Skipped: skipped,
		Missing: missing,
	}
}
