package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// publicPageSize is the public blog index page size.
const publicPageSize = 9

// PostPublicService is the subset of *posts.Service the public handler calls.
type PostPublicService interface {
	PublicBySlug(ctx context.Context, slug string) (posts.Post, error)
	PublicList(ctx context.Context, limit, offset int) ([]posts.Post, int, error)
	Like(ctx context.Context, postID, userID uuid.UUID) (posts.Post, error)
	Unlike(ctx context.Context, postID, userID uuid.UUID) (posts.Post, error)
	HasLiked(ctx context.Context, postID, userID uuid.UUID) (bool, error)
}

// PostPublicHandler is the thin HTTP boundary for the public blog. It renders
// the post detail + index using the public Base layout. The like endpoints are
// signed-in only (gated upstream by RequireAuth).
type PostPublicHandler struct {
	svc      PostPublicService
	authors  AuthorNamer
	siteName string
	baseURL  string
	csrf     func(*http.Request) string
}

// NewPostPublicHandler constructs the public posts handler.
func NewPostPublicHandler(svc PostPublicService, authors AuthorNamer, siteName, baseURL string, csrf func(*http.Request) string) *PostPublicHandler {
	if siteName == "" {
		siteName = "CMStack"
	}
	return &PostPublicHandler{svc: svc, authors: authors, siteName: siteName, baseURL: baseURL, csrf: csrf}
}

// Index renders the paginated published-posts grid.
func (h *PostPublicHandler) Index(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	items, total, err := h.svc.PublicList(r.Context(), publicPageSize, (page-1)*publicPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	cards := make([]webtempl.PublicPostCard, 0, len(items))
	for _, p := range items {
		cards = append(cards, webtempl.PublicPostCard{
			Title:       p.Title,
			URL:         "/blog/" + p.Slug,
			Excerpt:     p.Excerpt,
			AuthorName:  h.authorName(r.Context(), p.AuthorID),
			Date:        publishedDate(p),
			ReadingTime: p.ReadingTime,
		})
	}
	view := webtempl.PublicPostIndexView{
		SiteName: h.siteName,
		HomeURL:  "/",
		Cards:    cards,
		Pager:    pager(page, publicPageSize, total, "/blog", ""),
	}
	h.render(w, r, webtempl.PublicPostIndex(view))
}

// Show renders a single published post by slug.
func (h *PostPublicHandler) Show(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	post, err := h.svc.PublicBySlug(r.Context(), slug)
	if errors.Is(err, posts.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.PublicPostDetail(h.detailView(r, post)))
}

// Like records the current user's like and re-renders the like island (htmx).
func (h *PostPublicHandler) Like(w http.ResponseWriter, r *http.Request) {
	h.toggleLike(w, r, true)
}

// Unlike removes the current user's like and re-renders the island.
func (h *PostPublicHandler) Unlike(w http.ResponseWriter, r *http.Request) {
	h.toggleLike(w, r, false)
}

func (h *PostPublicHandler) toggleLike(w http.ResponseWriter, r *http.Request, like bool) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	slug := chi.URLParam(r, "slug")
	post, err := h.svc.PublicBySlug(r.Context(), slug)
	if errors.Is(err, posts.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	if like {
		post, err = h.svc.Like(r.Context(), post.ID, u.ID)
	} else {
		post, err = h.svc.Unlike(r.Context(), post.ID, u.ID)
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	view := h.detailView(r, post)
	view.Liked = like
	// htmx swaps just the like island; a non-htmx POST falls back to the full page.
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, webtempl.LikeIsland(view))
		return
	}
	http.Redirect(w, r, "/blog/"+slug, http.StatusSeeOther)
}

func (h *PostPublicHandler) detailView(r *http.Request, p posts.Post) webtempl.PublicPostView {
	u, signedIn := UserFromContext(r.Context())
	liked := false
	if signedIn {
		liked, _ = h.svc.HasLiked(r.Context(), p.ID, u.ID)
	}
	publishedAt := p.UpdatedAt
	if p.PublishedAt != nil {
		publishedAt = *p.PublishedAt
	}
	return webtempl.PublicPostView{
		SiteName:     h.siteName,
		HomeURL:      "/",
		Title:        p.Title,
		Slug:         p.Slug,
		BodyHTML:     p.Body,
		Excerpt:      p.Excerpt,
		AuthorID:     p.AuthorID.String(),
		AuthorName:   h.authorName(r.Context(), p.AuthorID),
		AuthorURL:    "/authors/" + p.AuthorID.String(),
		PublishedAt:  publishedAt,
		ReadingTime:  p.ReadingTime,
		LikeCount:    p.LikeCount,
		Liked:        liked,
		CanLike:      signedIn,
		LikeURL:      "/blog/" + p.Slug + "/like",
		CSRFToken:    h.csrf(r),
		CanonicalURL: h.baseURL + "/blog/" + p.Slug,
	}
}

func (h *PostPublicHandler) authorName(ctx context.Context, id uuid.UUID) string {
	if h.authors == nil {
		return "Author"
	}
	u, err := h.authors.GetByID(ctx, id)
	if err != nil {
		return "Author"
	}
	if u.Name != "" {
		return u.Name
	}
	if u.Username != "" {
		return u.Username
	}
	return "Author"
}

func (h *PostPublicHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func publishedDate(p posts.Post) string {
	if p.PublishedAt != nil {
		return p.PublishedAt.Format("January 2, 2006")
	}
	return p.CreatedAt.Format("January 2, 2006")
}

// decodeRevisionSnapshot decodes a stored revision snapshot for display. A
// malformed snapshot yields a zero value (the diff view shows it as empty).
func decodeRevisionSnapshot(raw json.RawMessage) snapshotView {
	var s snapshotView
	_ = json.Unmarshal(raw, &s)
	return s
}
