package api

import (
	"errors"
	"net/http"

	"github.com/huseyn0w/agentic-cms-go/internal/content/tags"
)

// listTags serves GET /api/v1/tags: a paginated tag listing.
func (h *handler) listTags(w http.ResponseWriter, r *http.Request) {
	page, perPage := paginate(r)
	items, total, err := h.tags.AdminList(r.Context(), perPage, (page-1)*perPage)
	if err != nil {
		Fail(w, http.StatusInternalServerError, "internal", "failed to list tags")
		return
	}
	dtos := make([]tagDTO, 0, len(items))
	for _, t := range items {
		dtos = append(dtos, toTagDTO(t))
	}
	OK(w, http.StatusOK, listResponse{Items: dtos, Total: total, Page: page, PerPage: perPage})
}

// createTagRequest is the JSON body for POST /api/v1/tags.
type createTagRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// createTag serves POST /api/v1/tags.
func (h *handler) createTag(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createTagRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}
	t, err := h.tags.Create(r.Context(), actorID, tags.CreateInput{Name: req.Name, Slug: req.Slug})
	if err != nil {
		writeTagError(w, err)
		return
	}
	OK(w, http.StatusCreated, toTagDTO(t))
}

// updateTagRequest is the JSON body for PATCH /api/v1/tags/{id}.
type updateTagRequest struct {
	Name *string `json:"name"`
	Slug *string `json:"slug"`
}

// updateTag serves PATCH /api/v1/tags/{id}.
func (h *handler) updateTag(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "tag")
	if !ok {
		return
	}
	var req updateTagRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}
	t, err := h.tags.Update(r.Context(), actorID, id, tags.UpdateInput{Name: req.Name, Slug: req.Slug})
	if err != nil {
		writeTagError(w, err)
		return
	}
	OK(w, http.StatusOK, toTagDTO(t))
}

// deleteTag serves DELETE /api/v1/tags/{id}: a hard delete.
func (h *handler) deleteTag(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "tag")
	if !ok {
		return
	}
	if err := h.tags.Delete(r.Context(), actorID, id); err != nil {
		writeTagError(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{"id": id.String(), "deleted": true})
}

// writeTagError maps a tag service error onto the JSON envelope.
func writeTagError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tags.ErrNotFound), errors.Is(err, tags.ErrForbidden):
		Fail(w, http.StatusNotFound, "not_found", "tag not found")
	case errors.Is(err, tags.ErrNameRequired):
		FailValidation(w, map[string]string{"name": "name is required"})
	default:
		Fail(w, http.StatusInternalServerError, "internal", "failed to process tag")
	}
}
