package web

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
)

// taxonomyBulkFunc is the delete-only bulk operation shared by the category and
// tag admin handlers. Both *categories.Service.BulkDelete and
// *tags.Service.BulkDelete satisfy it.
type taxonomyBulkFunc func(ctx context.Context, actor uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)

// handleTaxonomyBulk is the thin bulk endpoint for categories/tags. Unlike the
// content handleBulk (trash/restore/publish lifecycle) the taxonomy types only
// support delete, so the action is fixed and only the ids are read from the
// form. Per-id permission is re-checked inside the service (reused single-item
// Delete); the route gate already required the coarse delete grant.
func handleTaxonomyBulk(w http.ResponseWriter, r *http.Request, del taxonomyBulkFunc, redirectTo string) {
	u, _ := UserFromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	action := r.PostFormValue("action")
	if action != "delete" {
		http.Error(w, "unknown bulk action", http.StatusBadRequest)
		return
	}

	ids := parseBulkIDs(r.PostForm["ids"])
	if len(ids) == 0 {
		http.Redirect(w, r, redirectTo, http.StatusSeeOther)
		return
	}

	res, err := del(r.Context(), u.ID, ids)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, redirectTo+"?"+bulkSummaryQuery(bulkDelete, res), http.StatusSeeOther)
}
