package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/comments"
)

// listComments serves GET /api/v1/comments: a filtered, paginated moderation
// listing. The optional ?status= filter (pending/approved/spam/trashed) is
// forwarded to the service; an unknown/absent value lists all statuses.
func (h *handler) listComments(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	page, perPage := paginate(r)
	f := comments.ModerationFilter{
		Status: commentStatusParam(r),
		Limit:  perPage,
		Offset: (page - 1) * perPage,
	}
	items, total, err := h.comments.AdminList(r.Context(), actorID, f)
	if err != nil {
		writeCommentError(w, err)
		return
	}
	dtos := make([]commentDTO, 0, len(items))
	for _, c := range items {
		dtos = append(dtos, toCommentDTO(c))
	}
	OK(w, http.StatusOK, listResponse{Items: dtos, Total: total, Page: page, PerPage: perPage})
}

// approveComment serves POST /api/v1/comments/{id}/approve.
func (h *handler) approveComment(w http.ResponseWriter, r *http.Request) {
	h.commentStatusChange(w, r, func(actorID, id uuid.UUID) (comments.Comment, error) {
		return h.comments.Approve(r.Context(), actorID, id)
	})
}

// spamComment serves POST /api/v1/comments/{id}/spam.
func (h *handler) spamComment(w http.ResponseWriter, r *http.Request) {
	h.commentStatusChange(w, r, func(actorID, id uuid.UUID) (comments.Comment, error) {
		return h.comments.Spam(r.Context(), actorID, id)
	})
}

// trashComment serves POST /api/v1/comments/{id}/trash.
func (h *handler) trashComment(w http.ResponseWriter, r *http.Request) {
	h.commentStatusChange(w, r, func(actorID, id uuid.UUID) (comments.Comment, error) {
		return h.comments.Trash(r.Context(), actorID, id)
	})
}

// deleteComment serves DELETE /api/v1/comments/{id}: a permanent delete.
func (h *handler) deleteComment(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "comment")
	if !ok {
		return
	}
	if err := h.comments.Delete(r.Context(), actorID, id); err != nil {
		writeCommentError(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{"id": id.String(), "deleted": true})
}

// commentStatusChange runs an approve/spam/trash op and writes the comment DTO.
func (h *handler) commentStatusChange(w http.ResponseWriter, r *http.Request, fn func(actorID, id uuid.UUID) (comments.Comment, error)) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "comment")
	if !ok {
		return
	}
	c, err := fn(actorID, id)
	if err != nil {
		writeCommentError(w, err)
		return
	}
	OK(w, http.StatusOK, toCommentDTO(c))
}

// commentStatusParam maps the ?status= query to a moderation status pointer. It
// accepts both the canonical UPPERCASE tokens and their lowercase aliases
// (pending/approved/spam/trashed). An unknown/absent value yields nil (all
// statuses); "trashed" aliases the TRASH status.
func commentStatusParam(r *http.Request) *comments.Status {
	switch r.URL.Query().Get("status") {
	case "PENDING", "pending":
		s := comments.StatusPending
		return &s
	case "APPROVED", "approved":
		s := comments.StatusApproved
		return &s
	case "SPAM", "spam":
		s := comments.StatusSpam
		return &s
	case "TRASH", "trash", "trashed":
		s := comments.StatusTrash
		return &s
	default:
		return nil
	}
}

// writeCommentError maps a comment service error onto the JSON envelope.
func writeCommentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, comments.ErrNotFound):
		Fail(w, http.StatusNotFound, "not_found", "comment not found")
	case errors.Is(err, comments.ErrForbidden):
		Fail(w, http.StatusForbidden, "forbidden", "you do not have permission to do that")
	default:
		Fail(w, http.StatusInternalServerError, "internal", "failed to process comment")
	}
}
