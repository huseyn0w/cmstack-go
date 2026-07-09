package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/web"
)

// defaultPerPage is the page size used when none is supplied.
const defaultPerPage = 20

// maxPerPage caps the client-requested page size so a single call cannot pull an
// unbounded result set.
const maxPerPage = 100

// PostReader is the narrow read surface the posts endpoints need. *posts.Service
// satisfies it. Keeping it narrow decouples the API from the full service.
type PostReader interface {
	AdminList(ctx context.Context, f posts.ListFilter) ([]posts.Post, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (posts.Post, error)
}

// PageReader is the narrow read surface the pages endpoints need. *pages.Service
// satisfies it.
type PageReader interface {
	AdminList(ctx context.Context, f pages.ListFilter) ([]pages.Page, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (pages.Page, error)
}

// Deps are the explicit dependencies the API router needs. Auth is the shared
// RBAC middleware (the single source of truth); TokenAuth is the bearer-token
// authentication middleware that populates the request user; Posts/Pages are the
// narrow content readers.
type Deps struct {
	Auth      *web.AuthMiddleware
	TokenAuth func(http.Handler) http.Handler
	Posts     PostReader
	Pages     PageReader
}

// Mount registers the /api/v1 group on r. The group runs the bearer-token auth
// middleware for every route, then gates each endpoint with the existing
// RequirePermission RBAC check. It is mounted on the ROOT router (outside the
// session/CSRF group) because bearer auth is stateless and CSRF-exempt.
func Mount(r chi.Router, d Deps) {
	h := &handler{posts: d.Posts, pages: d.Pages}

	r.Route("/api/v1", func(ar chi.Router) {
		if d.TokenAuth != nil {
			ar.Use(d.TokenAuth)
		}

		if d.Posts != nil {
			ar.With(d.Auth.RequirePermission(accounts.ActionRead, accounts.SubjectPost)).
				Get("/posts", h.listPosts)
			ar.With(d.Auth.RequirePermission(accounts.ActionRead, accounts.SubjectPost)).
				Get("/posts/{id}", h.getPost)
		}
		if d.Pages != nil {
			ar.With(d.Auth.RequirePermission(accounts.ActionRead, accounts.SubjectPage)).
				Get("/pages", h.listPages)
			ar.With(d.Auth.RequirePermission(accounts.ActionRead, accounts.SubjectPage)).
				Get("/pages/{id}", h.getPage)
		}
	})
}

// handler holds the API's content readers. It carries no state beyond them.
type handler struct {
	posts PostReader
	pages PageReader
}

// listPosts serves GET /api/v1/posts: a filtered, paginated post listing.
func (h *handler) listPosts(w http.ResponseWriter, r *http.Request) {
	page, perPage := paginate(r)
	f := posts.ListFilter{
		IncludeTrashed: boolParam(r, "includeTrashed"),
		Limit:          perPage,
		Offset:         (page - 1) * perPage,
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
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Fail(w, http.StatusBadRequest, "invalid_id", "the post id is not a valid uuid")
		return
	}
	p, err := h.posts.Get(r.Context(), actorID, id)
	if err != nil {
		if errors.Is(err, posts.ErrNotFound) || errors.Is(err, posts.ErrForbidden) {
			Fail(w, http.StatusNotFound, "not_found", "post not found")
			return
		}
		Fail(w, http.StatusInternalServerError, "internal", "failed to load post")
		return
	}
	OK(w, http.StatusOK, toPostDetailDTO(p))
}

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
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Fail(w, http.StatusBadRequest, "invalid_id", "the page id is not a valid uuid")
		return
	}
	p, err := h.pages.Get(r.Context(), actorID, id)
	if err != nil {
		if errors.Is(err, pages.ErrNotFound) {
			Fail(w, http.StatusNotFound, "not_found", "page not found")
			return
		}
		Fail(w, http.StatusInternalServerError, "internal", "failed to load page")
		return
	}
	OK(w, http.StatusOK, toPageDetailDTO(p))
}

// actor returns the authenticated user's id from context (set by the token-auth
// middleware). The second result is false when no user is present.
func actor(r *http.Request) (uuid.UUID, bool) {
	u, ok := web.UserFromContext(r.Context())
	if !ok {
		return uuid.Nil, false
	}
	return u.ID, true
}

// paginate reads page/perPage query params, applying defaults (page 1, perPage
// 20) and the perPage cap (100). Values below 1 fall back to the default.
func paginate(r *http.Request) (page, perPage int) {
	page = 1
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	perPage = defaultPerPage
	if v, err := strconv.Atoi(r.URL.Query().Get("perPage")); err == nil && v > 0 {
		perPage = v
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	return page, perPage
}

// statusParam parses the optional ?status= filter into a kernel.Status. It
// accepts only the canonical DRAFT/PUBLISHED tokens; anything else (including an
// absent param) yields ok=false so the listing is unfiltered by status.
func statusParam(r *http.Request) (kernel.Status, bool) {
	switch r.URL.Query().Get("status") {
	case string(kernel.StatusDraft):
		return kernel.StatusDraft, true
	case string(kernel.StatusPublished):
		return kernel.StatusPublished, true
	default:
		return "", false
	}
}

// boolParam reports whether the named query param is present and truthy.
func boolParam(r *http.Request, name string) bool {
	switch r.URL.Query().Get(name) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
