package web

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/comments"
	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// commentAdminPageSize is the moderation list page size.
const commentAdminPageSize = 20

// CommentsAdminService is the subset of *comments.Service the moderation admin
// handler calls. Declaring it here keeps the handler testable with a stub.
type CommentsAdminService interface {
	AdminList(ctx context.Context, actorID uuid.UUID, f comments.ModerationFilter) ([]comments.Comment, int, error)
	StatusCounts(ctx context.Context, actorID uuid.UUID) (map[comments.Status]int, error)
	Approve(ctx context.Context, actorID, id uuid.UUID) (comments.Comment, error)
	Spam(ctx context.Context, actorID, id uuid.UUID) (comments.Comment, error)
	Trash(ctx context.Context, actorID, id uuid.UUID) (comments.Comment, error)
	Delete(ctx context.Context, actorID, id uuid.UUID) error
	BulkApprove(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkSpam(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkTrash(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkDelete(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
}

// CommentPostTitler resolves a target post's title for the moderation row
// (best-effort). *posts.RepoPG satisfies it via GetByID. Optional: when nil the
// post column shows the raw id fragment.
type CommentPostTitler interface {
	Title(ctx context.Context, postID uuid.UUID) string
}

// CommentsAdminHandler is the thin HTTP boundary for the admin moderation area.
// It decodes, calls the service, and renders/redirects — ZERO business logic.
type CommentsAdminHandler struct {
	svc    CommentsAdminService
	shell  adminShellDeps
	titler CommentPostTitler // optional post-title resolver
	csrf   func(*http.Request) string
}

// NewCommentsAdminHandler constructs the moderation admin handler.
func NewCommentsAdminHandler(svc CommentsAdminService, shell adminShellDeps, titler CommentPostTitler, csrf func(*http.Request) string) *CommentsAdminHandler {
	return &CommentsAdminHandler{svc: svc, shell: shell, titler: titler, csrf: csrf}
}

// List renders the moderation table with status-filter tabs (Pending/Approved/
// Spam/Trash), a pending count badge, and pagination.
func (h *CommentsAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	page := pageParam(r)
	statusParam := r.URL.Query().Get("status")
	status := parseModerationStatus(statusParam)

	f := comments.ModerationFilter{
		Status: status,
		Limit:  commentAdminPageSize,
		Offset: (page - 1) * commentAdminPageSize,
	}
	items, total, err := h.svc.AdminList(r.Context(), u.ID, f)
	if errors.Is(err, comments.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	counts, err := h.svc.StatusCounts(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	shell := h.shell.buildShell(r, "Comments")
	shell.Nav = webtempl.SetNavBadge(shell.Nav, "Comments", counts[comments.StatusPending])

	view := webtempl.CommentModerationView{
		Shell:        shell,
		Rows:         h.rows(r.Context(), items),
		Tabs:         h.tabs(statusParam, counts),
		Pager:        pager(page, commentAdminPageSize, total, "/admin/comments", moderationStatusQuery(statusParam)),
		BulkURL:      "/admin/comments/bulk",
		Summary:      bulkSummaryFromQuery(r),
		CSRFToken:    h.csrf(r),
		PendingCount: counts[comments.StatusPending],
	}
	h.render(w, r, webtempl.CommentModeration(view))
}

// Approve sets a comment's status to APPROVED then redirects back to the list
// (preserving the active status filter). Spam/Trash/Delete mirror it.
func (h *CommentsAdminHandler) Approve(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error {
		_, err := h.svc.Approve(r.Context(), actor, id)
		return err
	})
}

// Spam marks the selected comment as spam then redirects to the list.
func (h *CommentsAdminHandler) Spam(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error {
		_, err := h.svc.Spam(r.Context(), actor, id)
		return err
	})
}

// Trash soft-trashes the selected comment then redirects to the list.
func (h *CommentsAdminHandler) Trash(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error {
		_, err := h.svc.Trash(r.Context(), actor, id)
		return err
	})
}

// Delete hard-deletes the selected comment then redirects to the list.
func (h *CommentsAdminHandler) Delete(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error {
		return h.svc.Delete(r.Context(), actor, id)
	})
}

// Bulk dispatches an allow-listed moderation bulk action over the submitted ids.
// Unknown actions are rejected with 400 BEFORE any service call. Per-id
// permission is re-checked inside the service (unauthorized/missing ids are
// skipped, not failed). It redirects back with an aria-live summary query.
func (h *CommentsAdminHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	action := r.PostFormValue("action")
	redirect := "/admin/comments" + moderationRedirectQuery(r)
	var fn func(ctx context.Context, actor uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	switch action {
	case "approve":
		fn = h.svc.BulkApprove
	case "spam":
		fn = h.svc.BulkSpam
	case "trash":
		fn = h.svc.BulkTrash
	case "delete":
		fn = h.svc.BulkDelete
	default:
		http.Error(w, "unknown bulk action", http.StatusBadRequest)
		return
	}

	ids := parseBulkIDs(r.PostForm["ids"])
	if len(ids) == 0 {
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}

	res, err := fn(r.Context(), u.ID, ids)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	sep := "?"
	if strings.Contains(redirect, "?") {
		sep = "&"
	}
	http.Redirect(w, r, redirect+sep+bulkSummaryQuery(bulkAction(action), res), http.StatusSeeOther)
}

// --- helpers -----------------------------------------------------------------

// mutate runs a single-comment moderation op then redirects to the list with the
// active status filter preserved. It maps domain errors to HTTP outcomes.
func (h *CommentsAdminHandler) mutate(w http.ResponseWriter, r *http.Request, fn func(actor, id uuid.UUID) error) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = fn(u.ID, id)
	switch {
	case errors.Is(err, comments.ErrForbidden):
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	case errors.Is(err, comments.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/comments"+moderationRedirectQuery(r), http.StatusSeeOther)
}

func (h *CommentsAdminHandler) rows(ctx context.Context, items []comments.Comment) []webtempl.CommentAdminRow {
	base := "/admin/comments/"
	rows := make([]webtempl.CommentAdminRow, 0, len(items))
	for _, c := range items {
		id := c.ID.String()
		rows = append(rows, webtempl.CommentAdminRow{
			ID:         id,
			AuthorName: c.AuthorName,
			PostTitle:  h.postTitle(ctx, c.PostID),
			Excerpt:    commentExcerpt(c.Body, 120),
			Status:     webtempl.CommentStatus(c.Status),
			Date:       c.CreatedAt.Format("Jan 2, 2006 15:04"),
			ApproveURL: base + id + "/approve",
			SpamURL:    base + id + "/spam",
			TrashURL:   base + id + "/trash",
			DeleteURL:  base + id + "/delete",
		})
	}
	return rows
}

// postTitle resolves a target post's title (best-effort). Falls back to a short
// id fragment when no titler is wired or the lookup fails.
func (h *CommentsAdminHandler) postTitle(ctx context.Context, postID uuid.UUID) string {
	if h.titler != nil {
		if t := h.titler.Title(ctx, postID); t != "" {
			return t
		}
	}
	s := postID.String()
	if len(s) > 8 {
		return "post " + s[:8]
	}
	return "post " + s
}

func (h *CommentsAdminHandler) tabs(active string, counts map[comments.Status]int) []webtempl.CommentModerationTab {
	mk := func(label, value string, status comments.Status, badge bool) webtempl.CommentModerationTab {
		href := "/admin/comments"
		if value != "" {
			href += "?status=" + value
		}
		return webtempl.CommentModerationTab{
			Label:     label,
			Value:     value,
			Href:      href,
			Active:    active == value,
			Count:     counts[status],
			ShowBadge: badge,
		}
	}
	return []webtempl.CommentModerationTab{
		mk("Pending", "PENDING", comments.StatusPending, true),
		mk("Approved", "APPROVED", comments.StatusApproved, false),
		mk("Spam", "SPAM", comments.StatusSpam, false),
		mk("Trash", "TRASH", comments.StatusTrash, false),
	}
}

func (h *CommentsAdminHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// parseModerationStatus maps the ?status= query to a domain status pointer. An
// empty/unknown value means "all statuses" (nil). Only the four known statuses
// are accepted so a tampered filter can never widen behavior.
func parseModerationStatus(s string) *comments.Status {
	switch comments.Status(s) {
	case comments.StatusPending, comments.StatusApproved, comments.StatusSpam, comments.StatusTrash:
		st := comments.Status(s)
		return &st
	default:
		return nil
	}
}

// moderationStatusQuery echoes a valid status filter back for the pager links.
func moderationStatusQuery(s string) string {
	if parseModerationStatus(s) != nil {
		return "status=" + s
	}
	return ""
}

// moderationRedirectQuery preserves the active status filter on a post-action
// redirect so the moderator stays on the same tab.
func moderationRedirectQuery(r *http.Request) string {
	s := r.URL.Query().Get("status")
	if s == "" {
		s = r.PostFormValue("status")
	}
	if q := moderationStatusQuery(s); q != "" {
		return "?" + q
	}
	return ""
}

// commentExcerpt returns up to max runes of the (already sanitized) body for the
// moderation row preview.
func commentExcerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}
