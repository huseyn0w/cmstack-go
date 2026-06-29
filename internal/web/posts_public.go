package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// publicPageSize is the public blog index page size.
const publicPageSize = 9

// PostPublicService is the subset of *posts.Service the public handler calls.
// The filtered list + related methods are M3 additions; the concrete
// *posts.Service satisfies them all.
type PostPublicService interface {
	PublicBySlug(ctx context.Context, slug string) (posts.Post, error)
	PublicList(ctx context.Context, limit, offset int) ([]posts.Post, int, error)
	PublicListFiltered(ctx context.Context, categorySlug, tagSlug string, limit, offset int) ([]posts.Post, int, error)
	Related(ctx context.Context, postID uuid.UUID, limit int) ([]posts.Post, error)
	Like(ctx context.Context, postID, userID uuid.UUID) (posts.Post, error)
	Unlike(ctx context.Context, postID, userID uuid.UUID) (posts.Post, error)
	HasLiked(ctx context.Context, postID, userID uuid.UUID) (bool, error)
}

// PostTaxonomyReader supplies a post's categories + tags for the public detail
// pills (M3). *categories.Service / *tags.Service satisfy the two halves via the
// post public handler's optional readers.
type PostTaxonomyReader interface {
	CategoriesForPost(ctx context.Context, postID uuid.UUID) ([]categories.Category, error)
}

// PostTagReader supplies a post's tags for the public detail pills (M3).
type PostTagReader interface {
	TagsForPost(ctx context.Context, postID uuid.UUID) ([]tags.Tag, error)
}

// PostPublicHandler is the thin HTTP boundary for the public blog. It renders
// the post detail + index using the public Base layout. The like endpoints are
// signed-in only (gated upstream by RequireAuth).
type PostPublicHandler struct {
	svc        PostPublicService
	authors    AuthorNamer
	categories PostTaxonomyReader // optional (M3)
	tags       PostTagReader      // optional (M3)
	siteName   string
	baseURL    string
	csrf       func(*http.Request) string
}

// NewPostPublicHandler constructs the public posts handler.
func NewPostPublicHandler(svc PostPublicService, authors AuthorNamer, siteName, baseURL string, csrf func(*http.Request) string) *PostPublicHandler {
	if siteName == "" {
		siteName = "CMStack"
	}
	return &PostPublicHandler{svc: svc, authors: authors, siteName: siteName, baseURL: baseURL, csrf: csrf}
}

// WithTaxonomy attaches the optional category + tag readers so the public detail
// renders taxonomy pills (M3). Returns the receiver for chaining.
func (h *PostPublicHandler) WithTaxonomy(cats PostTaxonomyReader, tagSvc PostTagReader) *PostPublicHandler {
	h.categories = cats
	h.tags = tagSvc
	return h
}

// Index renders the paginated published-posts grid. It accepts optional,
// combinable ?category={slug} and ?tag={slug} filters (M3, ts parity); when both
// are empty the listing is the full published set. Drafts/trashed are always
// excluded by the service query.
func (h *PostPublicHandler) Index(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	categorySlug := r.URL.Query().Get("category")
	tagSlug := r.URL.Query().Get("tag")

	var (
		items []posts.Post
		total int
		err   error
	)
	if categorySlug != "" || tagSlug != "" {
		items, total, err = h.svc.PublicListFiltered(r.Context(), categorySlug, tagSlug, publicPageSize, (page-1)*publicPageSize)
	} else {
		items, total, err = h.svc.PublicList(r.Context(), publicPageSize, (page-1)*publicPageSize)
	}
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
		Pager:    pager(page, publicPageSize, total, "/blog", blogFilterQuery(categorySlug, tagSlug)),
	}
	h.render(w, r, webtempl.PublicPostIndex(view))
}

// blogFilterQuery preserves the active category/tag filters across pagination.
func blogFilterQuery(categorySlug, tagSlug string) string {
	q := url.Values{}
	if categorySlug != "" {
		q.Set("category", categorySlug)
	}
	if tagSlug != "" {
		q.Set("tag", tagSlug)
	}
	return q.Encode()
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
		Categories:   h.categoryPills(r.Context(), p.ID),
		Tags:         h.tagPills(r.Context(), p.ID),
		Related:      h.relatedCards(r.Context(), p.ID),
	}
}

// categoryPills returns the post's categories as archive-linking pills.
func (h *PostPublicHandler) categoryPills(ctx context.Context, postID uuid.UUID) []webtempl.TaxonomyPill {
	if h.categories == nil {
		return nil
	}
	cats, err := h.categories.CategoriesForPost(ctx, postID)
	if err != nil {
		return nil
	}
	pills := make([]webtempl.TaxonomyPill, 0, len(cats))
	for _, c := range cats {
		pills = append(pills, webtempl.TaxonomyPill{Label: c.Name, URL: "/categories/" + c.Slug})
	}
	return pills
}

// tagPills returns the post's tags as archive-linking pills.
func (h *PostPublicHandler) tagPills(ctx context.Context, postID uuid.UUID) []webtempl.TaxonomyPill {
	if h.tags == nil {
		return nil
	}
	ts, err := h.tags.TagsForPost(ctx, postID)
	if err != nil {
		return nil
	}
	pills := make([]webtempl.TaxonomyPill, 0, len(ts))
	for _, t := range ts {
		pills = append(pills, webtempl.TaxonomyPill{Label: t.Name, URL: "/tags/" + t.Slug})
	}
	return pills
}

// relatedCards returns up to 4 published posts sharing >=1 category/tag with
// this post (laravel parity).
func (h *PostPublicHandler) relatedCards(ctx context.Context, postID uuid.UUID) []webtempl.PublicPostCard {
	related, err := h.svc.Related(ctx, postID, 4)
	if err != nil || len(related) == 0 {
		return nil
	}
	cards := make([]webtempl.PublicPostCard, 0, len(related))
	for _, p := range related {
		cards = append(cards, webtempl.PublicPostCard{
			Title:       p.Title,
			URL:         "/blog/" + p.Slug,
			Excerpt:     p.Excerpt,
			AuthorName:  h.authorName(ctx, p.AuthorID),
			Date:        publishedDate(p),
			ReadingTime: p.ReadingTime,
		})
	}
	return cards
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
