package api

import (
	"errors"
	"net/http"

	"github.com/huseyn0w/agentic-cms-go/internal/content/media"
)

// listMedia serves GET /api/v1/media: a paginated asset listing (newest first).
func (h *handler) listMedia(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	page, perPage := paginate(r)
	items, total, err := h.media.List(r.Context(), actorID, perPage, (page-1)*perPage)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	dtos := make([]mediaDTO, 0, len(items))
	for _, m := range items {
		dtos = append(dtos, toMediaDTO(m, h.media.URL))
	}
	OK(w, http.StatusOK, listResponse{Items: dtos, Total: total, Page: page, PerPage: perPage})
}

// getMedia serves GET /api/v1/media/{id}: a single asset.
func (h *handler) getMedia(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "media")
	if !ok {
		return
	}
	m, err := h.media.Get(r.Context(), actorID, id)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	OK(w, http.StatusOK, toMediaDTO(m, h.media.URL))
}

// updateMediaRequest is the JSON body for PATCH /api/v1/media/{id}. ONLY the
// display metadata is editable; any other field would be rejected by the strict
// decoder (DisallowUnknownFields), so non-metadata fields can never be updated.
type updateMediaRequest struct {
	Alt     *string `json:"alt"`
	Title   *string `json:"title"`
	Caption *string `json:"caption"`
}

// updateMedia serves PATCH /api/v1/media/{id}: alt/title/caption metadata only.
// Omitted fields keep their current value (the current asset is loaded first).
func (h *handler) updateMedia(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "media")
	if !ok {
		return
	}
	var req updateMediaRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}

	current, err := h.media.Get(r.Context(), actorID, id)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	alt := valueOr(req.Alt, current.Alt)
	title := valueOr(req.Title, current.Title)
	caption := valueOr(req.Caption, current.Caption)

	m, err := h.media.UpdateMetadata(r.Context(), actorID, id, alt, title, caption)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	OK(w, http.StatusOK, toMediaDTO(m, h.media.URL))
}

// deleteMedia serves DELETE /api/v1/media/{id}: removes the asset (blobs + row).
func (h *handler) deleteMedia(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "media")
	if !ok {
		return
	}
	if err := h.media.Delete(r.Context(), actorID, id); err != nil {
		writeMediaError(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{"id": id.String(), "deleted": true})
}

// writeMediaError maps a media service error onto the JSON envelope.
func writeMediaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, media.ErrNotFound), errors.Is(err, media.ErrForbidden):
		Fail(w, http.StatusNotFound, "not_found", "media not found")
	default:
		Fail(w, http.StatusInternalServerError, "internal", "failed to process media")
	}
}

// valueOr returns the pointed-to value when set, else the fallback.
func valueOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}
