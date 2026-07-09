package api

import (
	"errors"
	"net/http"

	"github.com/huseyn0w/cmstack-go/internal/content/categories"
)

// listCategories serves GET /api/v1/categories: the full, flat category list.
func (h *handler) listCategories(w http.ResponseWriter, r *http.Request) {
	items, err := h.categories.AllFlat(r.Context())
	if err != nil {
		Fail(w, http.StatusInternalServerError, "internal", "failed to list categories")
		return
	}
	dtos := make([]categoryDTO, 0, len(items))
	for _, c := range items {
		dtos = append(dtos, toCategoryDTO(c))
	}
	OK(w, http.StatusOK, dtos)
}

// createCategoryRequest is the JSON body for POST /api/v1/categories.
type createCategoryRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description string  `json:"description"`
	ParentID    *string `json:"parentId"`
}

// createCategory serves POST /api/v1/categories.
func (h *handler) createCategory(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createCategoryRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}
	parent, ok := parseParentID(w, req.ParentID)
	if !ok {
		return
	}

	c, err := h.categories.Create(r.Context(), actorID, categories.CreateInput{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		ParentID:    parent,
	})
	if err != nil {
		writeCategoryError(w, err)
		return
	}
	OK(w, http.StatusCreated, toCategoryDTO(c))
}

// updateCategoryRequest is the JSON body for PATCH /api/v1/categories/{id}.
type updateCategoryRequest struct {
	Name        *string  `json:"name"`
	Slug        *string  `json:"slug"`
	Description *string  `json:"description"`
	ParentID    **string `json:"parentId"`
}

// updateCategory serves PATCH /api/v1/categories/{id}.
func (h *handler) updateCategory(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "category")
	if !ok {
		return
	}
	var req updateCategoryRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}

	in := categories.UpdateInput{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
	}
	if req.ParentID != nil {
		parent, ok := parseParentID(w, *req.ParentID)
		if !ok {
			return
		}
		in.SetParent = true
		in.ParentID = parent
	}

	c, err := h.categories.Update(r.Context(), actorID, id, in)
	if err != nil {
		writeCategoryError(w, err)
		return
	}
	OK(w, http.StatusOK, toCategoryDTO(c))
}

// deleteCategory serves DELETE /api/v1/categories/{id}: a hard delete.
func (h *handler) deleteCategory(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "category")
	if !ok {
		return
	}
	if err := h.categories.Delete(r.Context(), actorID, id); err != nil {
		writeCategoryError(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{"id": id.String(), "deleted": true})
}

// writeCategoryError maps a category service error onto the JSON envelope.
func writeCategoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, categories.ErrNotFound), errors.Is(err, categories.ErrForbidden):
		Fail(w, http.StatusNotFound, "not_found", "category not found")
	case errors.Is(err, categories.ErrNameRequired):
		FailValidation(w, map[string]string{"name": "name is required"})
	case errors.Is(err, categories.ErrParentNotFound):
		FailValidation(w, map[string]string{"parentId": "parent not found"})
	case errors.Is(err, categories.ErrParentCycle):
		FailValidation(w, map[string]string{"parentId": "parent would create a cycle"})
	default:
		Fail(w, http.StatusInternalServerError, "internal", "failed to process category")
	}
}
