package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// PublicAuthorService is the subset of *accounts.ProfileService the public
// author page needs.
type PublicAuthorService interface {
	PublicAuthor(ctx context.Context, id uuid.UUID) (accounts.PublicAuthor, error)
}

// AuthorPostsProvider lists an author's PUBLISHED posts for the public profile
// page. *posts.Service satisfies it. It is optional: when nil, the author page
// renders with the empty-posts seam from M1.
type AuthorPostsProvider interface {
	PublishedByAuthor(ctx context.Context, authorID uuid.UUID) ([]posts.Post, error)
}

// AuthorHandler is the thin HTTP boundary for the public author profile page. It
// resolves the id, calls the service, and renders the public templ page. The
// payload it renders carries NO email (the service omits it by construction).
type AuthorHandler struct {
	svc      PublicAuthorService
	posts    AuthorPostsProvider
	siteName string
	homeURL  string
}

// NewAuthorHandler constructs the public author handler. postsProvider may be
// nil (the posts section then shows the empty-state seam).
func NewAuthorHandler(svc PublicAuthorService, postsProvider AuthorPostsProvider, siteName, homeURL string) *AuthorHandler {
	if siteName == "" {
		siteName = "CMStack"
	}
	if homeURL == "" {
		homeURL = "/"
	}
	return &AuthorHandler{svc: svc, posts: postsProvider, siteName: siteName, homeURL: homeURL}
}

// Show renders the public author page for the {id} path param. An unknown or
// malformed id is a 404 (no information disclosure about which it was).
func (h *AuthorHandler) Show(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	author, err := h.svc.PublicAuthor(r.Context(), id)
	if errors.Is(err, accounts.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	view := webtempl.AuthorPageView{
		ProfilePageView: webtempl.ProfilePageView{
			Name:        author.Name,
			Bio:         author.Bio,
			AvatarURL:   author.AvatarURL,
			Website:     author.Website,
			ProfileURL:  h.profileURL(author.ID),
			SocialOrder: accounts.SocialOrder(author.SocialLinks),
			Socials:     author.SocialLinks,
			RoleLabel:   author.RoleLabel,
			SiteName:    h.siteName,
			HomeURL:     h.homeURL,
		},
		Posts: h.authorPosts(r.Context(), author),
	}

	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.AuthorPage(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func (h *AuthorHandler) profileURL(id uuid.UUID) string {
	base := h.homeURL
	if base == "/" {
		base = ""
	}
	return base + "/authors/" + id.String()
}

// authorPosts lists the author's published posts via the posts provider (the
// M1 seam, now filled). When no provider is wired it falls back to the (empty)
// posts carried on the accounts.PublicAuthor payload.
func (h *AuthorHandler) authorPosts(ctx context.Context, author accounts.PublicAuthor) []webtempl.AuthorPostLink {
	if h.posts == nil {
		return mapAuthorPosts(author.Posts)
	}
	published, err := h.posts.PublishedByAuthor(ctx, author.ID)
	if err != nil {
		return mapAuthorPosts(author.Posts)
	}
	out := make([]webtempl.AuthorPostLink, 0, len(published))
	for _, p := range published {
		out = append(out, webtempl.AuthorPostLink{Title: p.Title, URL: "/blog/" + p.Slug})
	}
	return out
}

// mapAuthorPosts converts the accounts-layer post list to the view's link list
// (fallback path when no posts provider is wired).
func mapAuthorPosts(posts []accounts.AuthorPost) []webtempl.AuthorPostLink {
	out := make([]webtempl.AuthorPostLink, 0, len(posts))
	for _, p := range posts {
		out = append(out, webtempl.AuthorPostLink{Title: p.Title, URL: p.URL})
	}
	return out
}
