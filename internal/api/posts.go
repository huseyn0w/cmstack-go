package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
)

// listPosts serves GET /api/v1/posts: a filtered, paginated post listing. The
// categorySlug/tagSlug/q query params narrow the (admin, all-statuses) listing
// alongside the existing status/includeTrashed/pagination params.
func (h *handler) listPosts(w http.ResponseWriter, r *http.Request) {
	page, perPage := paginate(r)
	f := posts.ListFilter{
		IncludeTrashed: boolParam(r, "includeTrashed"),
		Limit:          perPage,
		Offset:         (page - 1) * perPage,
		CategorySlug:   strings.TrimSpace(r.URL.Query().Get("categorySlug")),
		TagSlug:        strings.TrimSpace(r.URL.Query().Get("tagSlug")),
		Q:              strings.TrimSpace(r.URL.Query().Get("q")),
	}
	if s, ok := statusParam(r); ok {
		f.Status = &s
	}

	items, total, err := h.posts.AdminList(r.Context(), f)
	if err != nil {
		Fail(w, http.StatusInternalServerError, "internal", "failed to list posts")
		return
	}
	dtos := make([]postDTO, 0, len(items))
	for _, p := range items {
		dtos = append(dtos, toPostDTO(p))
	}
	OK(w, http.StatusOK, listResponse{Items: dtos, Total: total, Page: page, PerPage: perPage})
}

// getPost serves GET /api/v1/posts/{id}: a single post with its full body.
func (h *handler) getPost(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "post")
	if !ok {
		return
	}
	p, err := h.posts.Get(r.Context(), actorID, id)
	if err != nil {
		writePostError(w, err)
		return
	}
	OK(w, http.StatusOK, toPostDetailDTO(p))
}

// listPostRevisions serves GET /api/v1/posts/{id}/revisions: the revision
// history (summary DTOs, no full body).
func (h *handler) listPostRevisions(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "post")
	if !ok {
		return
	}
	revs, err := h.posts.Revisions(r.Context(), actorID, id)
	if err != nil {
		writePostError(w, err)
		return
	}
	dtos := make([]revisionDTO, 0, len(revs))
	for _, rev := range revs {
		dtos = append(dtos, toRevisionDTO(rev))
	}
	OK(w, http.StatusOK, dtos)
}

// createPostRequest is the JSON body for POST /api/v1/posts.
type createPostRequest struct {
	Title           string   `json:"title"`
	Slug            string   `json:"slug"`
	Excerpt         string   `json:"excerpt"`
	Body            string   `json:"body"`
	Status          string   `json:"status"`
	CategoryIDs     []string `json:"categoryIds"`
	TagIDs          []string `json:"tagIds"`
	MetaTitle       string   `json:"metaTitle"`
	MetaDescription string   `json:"metaDescription"`
	CanonicalURL    string   `json:"canonicalUrl"`
	NoIndex         bool     `json:"noIndex"`
}

// createPost serves POST /api/v1/posts.
func (h *handler) createPost(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createPostRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}
	catIDs, err := parseUUIDs(req.CategoryIDs)
	if err != nil {
		FailValidation(w, map[string]string{"categoryIds": "must be an array of valid uuids"})
		return
	}
	tagIDs, err := parseUUIDs(req.TagIDs)
	if err != nil {
		FailValidation(w, map[string]string{"tagIds": "must be an array of valid uuids"})
		return
	}

	p, err := h.posts.Create(r.Context(), actorID, posts.CreateInput{
		Title:           req.Title,
		Slug:            req.Slug,
		Excerpt:         req.Excerpt,
		Body:            req.Body,
		Status:          kernel.ParseStatus(req.Status),
		CategoryIDs:     catIDs,
		TagIDs:          tagIDs,
		MetaTitle:       req.MetaTitle,
		MetaDescription: req.MetaDescription,
		CanonicalURL:    req.CanonicalURL,
		NoIndex:         req.NoIndex,
	})
	if err != nil {
		writePostError(w, err)
		return
	}
	OK(w, http.StatusCreated, toPostDetailDTO(p))
}

// updatePostRequest is the JSON body for PATCH /api/v1/posts/{id}. Every field
// is a pointer so an omitted field leaves the stored value unchanged.
type updatePostRequest struct {
	Title           *string   `json:"title"`
	Slug            *string   `json:"slug"`
	Excerpt         *string   `json:"excerpt"`
	Body            *string   `json:"body"`
	Status          *string   `json:"status"`
	CategoryIDs     *[]string `json:"categoryIds"`
	TagIDs          *[]string `json:"tagIds"`
	MetaTitle       *string   `json:"metaTitle"`
	MetaDescription *string   `json:"metaDescription"`
	CanonicalURL    *string   `json:"canonicalUrl"`
	NoIndex         *bool     `json:"noIndex"`
}

// updatePost serves PATCH /api/v1/posts/{id}.
func (h *handler) updatePost(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "post")
	if !ok {
		return
	}
	var req updatePostRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}

	in := posts.UpdateInput{
		Title:           req.Title,
		Slug:            req.Slug,
		Excerpt:         req.Excerpt,
		Body:            req.Body,
		MetaTitle:       req.MetaTitle,
		MetaDescription: req.MetaDescription,
		CanonicalURL:    req.CanonicalURL,
		NoIndex:         req.NoIndex,
	}
	if req.Status != nil {
		s := kernel.ParseStatus(*req.Status)
		in.Status = &s
	}
	// Taxonomy is REPLACED only when at least one of the axes is present in the
	// body; supplying either sets both (an omitted axis clears that side).
	if req.CategoryIDs != nil || req.TagIDs != nil {
		catIDs, err := parseUUIDs(deref(req.CategoryIDs))
		if err != nil {
			FailValidation(w, map[string]string{"categoryIds": "must be an array of valid uuids"})
			return
		}
		tagIDs, err := parseUUIDs(deref(req.TagIDs))
		if err != nil {
			FailValidation(w, map[string]string{"tagIds": "must be an array of valid uuids"})
			return
		}
		in.SetTaxonomy = true
		in.CategoryIDs = catIDs
		in.TagIDs = tagIDs
	}

	p, err := h.posts.Update(r.Context(), actorID, id, in)
	if err != nil {
		writePostError(w, err)
		return
	}
	OK(w, http.StatusOK, toPostDetailDTO(p))
}

// publishPost serves POST /api/v1/posts/{id}/publish.
func (h *handler) publishPost(w http.ResponseWriter, r *http.Request) {
	h.postStatusChange(w, r, func(actorID, id uuid.UUID) (posts.Post, error) {
		return h.posts.Publish(r.Context(), actorID, id)
	})
}

// unpublishPost serves POST /api/v1/posts/{id}/unpublish.
func (h *handler) unpublishPost(w http.ResponseWriter, r *http.Request) {
	h.postStatusChange(w, r, func(actorID, id uuid.UUID) (posts.Post, error) {
		return h.posts.Unpublish(r.Context(), actorID, id)
	})
}

// restorePost serves POST /api/v1/posts/{id}/restore. The restored post is
// reloaded so the response carries the full detail DTO.
func (h *handler) restorePost(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "post")
	if !ok {
		return
	}
	if err := h.posts.Restore(r.Context(), actorID, id); err != nil {
		writePostError(w, err)
		return
	}
	p, err := h.posts.Get(r.Context(), actorID, id)
	if err != nil {
		writePostError(w, err)
		return
	}
	OK(w, http.StatusOK, toPostDetailDTO(p))
}

// trashPost serves DELETE /api/v1/posts/{id}: a SOFT delete (trash). The
// response confirms the trashed id (matching the ts delete_post soft-trash
// semantics).
func (h *handler) trashPost(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "post")
	if !ok {
		return
	}
	if err := h.posts.Trash(r.Context(), actorID, id); err != nil {
		writePostError(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{"id": id.String(), "trashed": true})
}

// postStatusChange runs a publish/unpublish op and writes the detail DTO.
func (h *handler) postStatusChange(w http.ResponseWriter, r *http.Request, fn func(actorID, id uuid.UUID) (posts.Post, error)) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "post")
	if !ok {
		return
	}
	p, err := fn(actorID, id)
	if err != nil {
		writePostError(w, err)
		return
	}
	OK(w, http.StatusOK, toPostDetailDTO(p))
}

// writePostError maps a post service error onto the uniform JSON envelope:
// not-found/forbidden -> 404, title-required -> 422, everything else -> 500.
func writePostError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, posts.ErrNotFound), errors.Is(err, posts.ErrForbidden):
		Fail(w, http.StatusNotFound, "not_found", "post not found")
	case errors.Is(err, posts.ErrTitleRequired):
		FailValidation(w, map[string]string{"title": "title is required"})
	case errors.Is(err, posts.ErrSlugTaken):
		FailValidation(w, map[string]string{"slug": "slug is already taken"})
	default:
		Fail(w, http.StatusInternalServerError, "internal", "failed to process post")
	}
}

// deref returns the pointed-to slice or nil when the pointer is nil.
func deref(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}
