package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/content/pages"
)

// listPages serves GET /api/v1/pages: a filtered, paginated page listing.
func (h *handler) listPages(w http.ResponseWriter, r *http.Request) {
	page, perPage := paginate(r)
	f := pages.ListFilter{
		Limit:  perPage,
		Offset: (page - 1) * perPage,
	}
	if s, ok := statusParam(r); ok {
		f.Status = &s
	}

	items, total, err := h.pages.AdminList(r.Context(), f)
	if err != nil {
		Fail(w, http.StatusInternalServerError, "internal", "failed to list pages")
		return
	}
	dtos := make([]pageDTO, 0, len(items))
	for _, p := range items {
		dtos = append(dtos, toPageDTO(p))
	}
	OK(w, http.StatusOK, listResponse{Items: dtos, Total: total, Page: page, PerPage: perPage})
}

// getPage serves GET /api/v1/pages/{id}: a single page with its full body.
func (h *handler) getPage(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "page")
	if !ok {
		return
	}
	p, err := h.pages.Get(r.Context(), actorID, id)
	if err != nil {
		writePageError(w, err)
		return
	}
	OK(w, http.StatusOK, toPageDetailDTO(p))
}

// createPageRequest is the JSON body for POST /api/v1/pages.
type createPageRequest struct {
	Title           string  `json:"title"`
	Slug            string  `json:"slug"`
	Body            string  `json:"body"`
	Status          string  `json:"status"`
	Template        string  `json:"template"`
	ParentID        *string `json:"parentId"`
	MetaTitle       string  `json:"metaTitle"`
	MetaDescription string  `json:"metaDescription"`
	CanonicalURL    string  `json:"canonicalUrl"`
	NoIndex         bool    `json:"noIndex"`
}

// createPage serves POST /api/v1/pages.
func (h *handler) createPage(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createPageRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}
	parent, ok := parseParentID(w, req.ParentID)
	if !ok {
		return
	}

	p, err := h.pages.Create(r.Context(), actorID, pages.CreateInput{
		Title:           req.Title,
		Slug:            req.Slug,
		Body:            req.Body,
		Status:          kernel.ParseStatus(req.Status),
		ParentID:        parent,
		Template:        req.Template,
		MetaTitle:       req.MetaTitle,
		MetaDescription: req.MetaDescription,
		CanonicalURL:    req.CanonicalURL,
		NoIndex:         req.NoIndex,
	})
	if err != nil {
		writePageError(w, err)
		return
	}
	OK(w, http.StatusCreated, toPageDetailDTO(p))
}

// updatePageRequest is the JSON body for PATCH /api/v1/pages/{id}. Pointer
// fields leave the stored value unchanged when omitted. parentId uses a
// pointer-to-pointer so "omitted" (leave), "null" (clear), and "an id" (set)
// are distinguishable.
type updatePageRequest struct {
	Title           *string  `json:"title"`
	Slug            *string  `json:"slug"`
	Body            *string  `json:"body"`
	Status          *string  `json:"status"`
	Template        *string  `json:"template"`
	ParentID        **string `json:"parentId"`
	MetaTitle       *string  `json:"metaTitle"`
	MetaDescription *string  `json:"metaDescription"`
	CanonicalURL    *string  `json:"canonicalUrl"`
	NoIndex         *bool    `json:"noIndex"`
}

// updatePage serves PATCH /api/v1/pages/{id}.
func (h *handler) updatePage(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "page")
	if !ok {
		return
	}
	var req updatePageRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}

	in := pages.UpdateInput{
		Title:           req.Title,
		Slug:            req.Slug,
		Body:            req.Body,
		Template:        req.Template,
		MetaTitle:       req.MetaTitle,
		MetaDescription: req.MetaDescription,
		CanonicalURL:    req.CanonicalURL,
		NoIndex:         req.NoIndex,
	}
	if req.Status != nil {
		s := kernel.ParseStatus(*req.Status)
		in.Status = &s
	}
	if req.ParentID != nil {
		parent, ok := parseParentID(w, *req.ParentID)
		if !ok {
			return
		}
		in.SetParent = true
		in.ParentID = parent
	}

	p, err := h.pages.Update(r.Context(), actorID, id, in)
	if err != nil {
		writePageError(w, err)
		return
	}
	OK(w, http.StatusOK, toPageDetailDTO(p))
}

// publishPage serves POST /api/v1/pages/{id}/publish.
func (h *handler) publishPage(w http.ResponseWriter, r *http.Request) {
	h.pageStatusChange(w, r, func(actorID, id uuid.UUID) (pages.Page, error) {
		return h.pages.Publish(r.Context(), actorID, id)
	})
}

// unpublishPage serves POST /api/v1/pages/{id}/unpublish.
func (h *handler) unpublishPage(w http.ResponseWriter, r *http.Request) {
	h.pageStatusChange(w, r, func(actorID, id uuid.UUID) (pages.Page, error) {
		return h.pages.Unpublish(r.Context(), actorID, id)
	})
}

// trashPage serves DELETE /api/v1/pages/{id}: a SOFT delete (trash).
func (h *handler) trashPage(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "page")
	if !ok {
		return
	}
	if err := h.pages.Trash(r.Context(), actorID, id); err != nil {
		writePageError(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{"id": id.String(), "trashed": true})
}

// restorePage serves POST /api/v1/pages/{id}/restore.
func (h *handler) restorePage(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "page")
	if !ok {
		return
	}
	if err := h.pages.Restore(r.Context(), actorID, id); err != nil {
		writePageError(w, err)
		return
	}
	p, err := h.pages.Get(r.Context(), actorID, id)
	if err != nil {
		writePageError(w, err)
		return
	}
	OK(w, http.StatusOK, toPageDetailDTO(p))
}

// pageStatusChange runs a publish/unpublish op and writes the detail DTO.
func (h *handler) pageStatusChange(w http.ResponseWriter, r *http.Request, fn func(actorID, id uuid.UUID) (pages.Page, error)) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "page")
	if !ok {
		return
	}
	p, err := fn(actorID, id)
	if err != nil {
		writePageError(w, err)
		return
	}
	OK(w, http.StatusOK, toPageDetailDTO(p))
}

// parseParentID converts an optional parent-id string into a *uuid.UUID: a nil
// input (or empty string) means "no parent"; a malformed value writes a 422 and
// returns ok=false.
func parseParentID(w http.ResponseWriter, raw *string) (*uuid.UUID, bool) {
	if raw == nil || *raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		FailValidation(w, map[string]string{"parentId": "must be a valid uuid"})
		return nil, false
	}
	return &id, true
}

// writePageError maps a page service error onto the uniform JSON envelope.
func writePageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pages.ErrNotFound), errors.Is(err, pages.ErrForbidden):
		Fail(w, http.StatusNotFound, "not_found", "page not found")
	case errors.Is(err, pages.ErrTitleRequired):
		FailValidation(w, map[string]string{"title": "title is required"})
	case errors.Is(err, pages.ErrParentNotFound):
		FailValidation(w, map[string]string{"parentId": "parent not found"})
	case errors.Is(err, pages.ErrParentCycle):
		FailValidation(w, map[string]string{"parentId": "parent would create a cycle"})
	default:
		Fail(w, http.StatusInternalServerError, "internal", "failed to process page")
	}
}
