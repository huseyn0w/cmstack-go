package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/comments"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/media"
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
	"github.com/huseyn0w/cmstack-go/internal/web"
)

// defaultPerPage is the page size used when none is supplied.
const defaultPerPage = 20

// maxPerPage caps the client-requested page size so a single call cannot pull an
// unbounded result set.
const maxPerPage = 100

// PostService is the narrow surface the posts endpoints need. *posts.Service
// satisfies it. Keeping it narrow decouples the API from the full service.
type PostService interface {
	AdminList(ctx context.Context, f posts.ListFilter) ([]posts.Post, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (posts.Post, error)
	Revisions(ctx context.Context, actorID, id uuid.UUID) ([]kernel.Revision, error)
	Create(ctx context.Context, authorID uuid.UUID, in posts.CreateInput) (posts.Post, error)
	Update(ctx context.Context, actorID, id uuid.UUID, in posts.UpdateInput) (posts.Post, error)
	Publish(ctx context.Context, actorID, id uuid.UUID) (posts.Post, error)
	Unpublish(ctx context.Context, actorID, id uuid.UUID) (posts.Post, error)
	Trash(ctx context.Context, actorID, id uuid.UUID) error
	Restore(ctx context.Context, actorID, id uuid.UUID) error
}

// PageService is the narrow surface the pages endpoints need. *pages.Service
// satisfies it.
type PageService interface {
	AdminList(ctx context.Context, f pages.ListFilter) ([]pages.Page, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (pages.Page, error)
	Create(ctx context.Context, actorID uuid.UUID, in pages.CreateInput) (pages.Page, error)
	Update(ctx context.Context, actorID, id uuid.UUID, in pages.UpdateInput) (pages.Page, error)
	Publish(ctx context.Context, actorID, id uuid.UUID) (pages.Page, error)
	Unpublish(ctx context.Context, actorID, id uuid.UUID) (pages.Page, error)
	Trash(ctx context.Context, actorID, id uuid.UUID) error
	Restore(ctx context.Context, actorID, id uuid.UUID) error
}

// CategoryService is the narrow surface the categories endpoints need.
// *categories.Service satisfies it.
type CategoryService interface {
	AllFlat(ctx context.Context) ([]categories.Category, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (categories.Category, error)
	Create(ctx context.Context, actorID uuid.UUID, in categories.CreateInput) (categories.Category, error)
	Update(ctx context.Context, actorID, id uuid.UUID, in categories.UpdateInput) (categories.Category, error)
	Delete(ctx context.Context, actorID, id uuid.UUID) error
}

// TagService is the narrow surface the tags endpoints need. *tags.Service
// satisfies it.
type TagService interface {
	AdminList(ctx context.Context, limit, offset int) ([]tags.Tag, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (tags.Tag, error)
	Create(ctx context.Context, actorID uuid.UUID, in tags.CreateInput) (tags.Tag, error)
	Update(ctx context.Context, actorID, id uuid.UUID, in tags.UpdateInput) (tags.Tag, error)
	Delete(ctx context.Context, actorID, id uuid.UUID) error
}

// MediaService is the narrow surface the media endpoints need. *media.Service
// satisfies it. Upload is intentionally excluded (multipart upload is out of
// this slice's scope).
type MediaService interface {
	List(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]media.Media, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (media.Media, error)
	UpdateMetadata(ctx context.Context, actorID, id uuid.UUID, alt, title, caption string) (media.Media, error)
	Delete(ctx context.Context, actorID, id uuid.UUID) error
	URL(key string) string
}

// CommentService is the narrow surface the comments endpoints need.
// *comments.Service satisfies it.
type CommentService interface {
	AdminList(ctx context.Context, actorID uuid.UUID, f comments.ModerationFilter) ([]comments.Comment, int, error)
	Approve(ctx context.Context, actorID, id uuid.UUID) (comments.Comment, error)
	Spam(ctx context.Context, actorID, id uuid.UUID) (comments.Comment, error)
	Trash(ctx context.Context, actorID, id uuid.UUID) (comments.Comment, error)
	Delete(ctx context.Context, actorID, id uuid.UUID) error
}

// Deps are the explicit dependencies the API router needs. Auth is the shared
// RBAC middleware (the single source of truth); TokenAuth is the bearer-token
// authentication middleware that populates the request user; the remaining
// fields are the narrow per-resource content services. Any nil service leaves
// its routes unmounted.
type Deps struct {
	Auth       *web.AuthMiddleware
	TokenAuth  func(http.Handler) http.Handler
	Posts      PostService
	Pages      PageService
	Categories CategoryService
	Tags       TagService
	Media      MediaService
	Comments   CommentService
}

// handler holds the API's content services. It carries no state beyond them.
type handler struct {
	posts      PostService
	pages      PageService
	categories CategoryService
	tags       TagService
	media      MediaService
	comments   CommentService
}

// Mount registers the /api/v1 group on r. The group runs the bearer-token auth
// middleware for every route, then gates each endpoint with the existing
// RequirePermission RBAC check. It is mounted on the ROOT router (outside the
// session/CSRF group) because bearer auth is stateless and CSRF-exempt.
func Mount(r chi.Router, d Deps) {
	h := &handler{
		posts:      d.Posts,
		pages:      d.Pages,
		categories: d.Categories,
		tags:       d.Tags,
		media:      d.Media,
		comments:   d.Comments,
	}

	r.Route("/api/v1", func(ar chi.Router) {
		if d.TokenAuth != nil {
			ar.Use(d.TokenAuth)
		}

		if d.Posts != nil {
			gate := func(action string) func(http.Handler) http.Handler {
				return d.Auth.RequirePermission(action, accounts.SubjectPost)
			}
			ar.With(gate(accounts.ActionRead)).Get("/posts", h.listPosts)
			ar.With(gate(accounts.ActionRead)).Get("/posts/{id}", h.getPost)
			ar.With(gate(accounts.ActionRead)).Get("/posts/{id}/revisions", h.listPostRevisions)
			ar.With(gate(accounts.ActionCreate)).Post("/posts", h.createPost)
			ar.With(gate(accounts.ActionUpdate)).Patch("/posts/{id}", h.updatePost)
			ar.With(gate(accounts.ActionPublish)).Post("/posts/{id}/publish", h.publishPost)
			ar.With(gate(accounts.ActionPublish)).Post("/posts/{id}/unpublish", h.unpublishPost)
			ar.With(gate(accounts.ActionDelete)).Delete("/posts/{id}", h.trashPost)
			ar.With(gate(accounts.ActionUpdate)).Post("/posts/{id}/restore", h.restorePost)
		}

		if d.Pages != nil {
			gate := func(action string) func(http.Handler) http.Handler {
				return d.Auth.RequirePermission(action, accounts.SubjectPage)
			}
			ar.With(gate(accounts.ActionRead)).Get("/pages", h.listPages)
			ar.With(gate(accounts.ActionRead)).Get("/pages/{id}", h.getPage)
			ar.With(gate(accounts.ActionCreate)).Post("/pages", h.createPage)
			ar.With(gate(accounts.ActionUpdate)).Patch("/pages/{id}", h.updatePage)
			ar.With(gate(accounts.ActionPublish)).Post("/pages/{id}/publish", h.publishPage)
			ar.With(gate(accounts.ActionPublish)).Post("/pages/{id}/unpublish", h.unpublishPage)
			ar.With(gate(accounts.ActionDelete)).Delete("/pages/{id}", h.trashPage)
			ar.With(gate(accounts.ActionUpdate)).Post("/pages/{id}/restore", h.restorePage)
		}

		if d.Categories != nil {
			gate := func(action string) func(http.Handler) http.Handler {
				return d.Auth.RequirePermission(action, accounts.SubjectCategory)
			}
			ar.With(gate(accounts.ActionRead)).Get("/categories", h.listCategories)
			ar.With(gate(accounts.ActionCreate)).Post("/categories", h.createCategory)
			ar.With(gate(accounts.ActionUpdate)).Patch("/categories/{id}", h.updateCategory)
			ar.With(gate(accounts.ActionDelete)).Delete("/categories/{id}", h.deleteCategory)
		}

		if d.Tags != nil {
			gate := func(action string) func(http.Handler) http.Handler {
				return d.Auth.RequirePermission(action, accounts.SubjectTag)
			}
			ar.With(gate(accounts.ActionRead)).Get("/tags", h.listTags)
			ar.With(gate(accounts.ActionCreate)).Post("/tags", h.createTag)
			ar.With(gate(accounts.ActionUpdate)).Patch("/tags/{id}", h.updateTag)
			ar.With(gate(accounts.ActionDelete)).Delete("/tags/{id}", h.deleteTag)
		}

		if d.Media != nil {
			gate := func(action string) func(http.Handler) http.Handler {
				return d.Auth.RequirePermission(action, accounts.SubjectMedia)
			}
			ar.With(gate(accounts.ActionRead)).Get("/media", h.listMedia)
			ar.With(gate(accounts.ActionRead)).Get("/media/{id}", h.getMedia)
			ar.With(gate(accounts.ActionUpdate)).Patch("/media/{id}", h.updateMedia)
			ar.With(gate(accounts.ActionDelete)).Delete("/media/{id}", h.deleteMedia)
		}

		if d.Comments != nil {
			gate := func(action string) func(http.Handler) http.Handler {
				return d.Auth.RequirePermission(action, accounts.SubjectComment)
			}
			ar.With(gate(accounts.ActionRead)).Get("/comments", h.listComments)
			ar.With(gate(accounts.ActionUpdate)).Post("/comments/{id}/approve", h.approveComment)
			ar.With(gate(accounts.ActionUpdate)).Post("/comments/{id}/spam", h.spamComment)
			ar.With(gate(accounts.ActionUpdate)).Post("/comments/{id}/trash", h.trashComment)
			ar.With(gate(accounts.ActionDelete)).Delete("/comments/{id}", h.deleteComment)
		}
	})
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
