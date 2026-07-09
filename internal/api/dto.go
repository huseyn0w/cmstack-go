package api

import (
	"time"

	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
)

// postDTO is the stable, public JSON shape of a post. It exposes only the fields
// the API contract needs; internal-only concerns (reading time, like count,
// deleted_at, raw SEO structural flags) are intentionally omitted so the DTO is
// a deliberate contract rather than a struct dump. Body is populated on detail
// reads and left empty ("") on list reads.
type postDTO struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Excerpt     string     `json:"excerpt"`
	Body        string     `json:"body,omitempty"`
	Status      string     `json:"status"`
	AuthorID    string     `json:"authorId"`
	PublishedAt *time.Time `json:"publishedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// toPostDTO maps a domain post onto the list DTO (no body).
func toPostDTO(p posts.Post) postDTO {
	return postDTO{
		ID:          p.ID.String(),
		Title:       p.Title,
		Slug:        p.Slug,
		Excerpt:     p.Excerpt,
		Status:      p.Status.String(),
		AuthorID:    p.AuthorID.String(),
		PublishedAt: p.PublishedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// toPostDetailDTO maps a domain post onto the detail DTO (full body).
func toPostDetailDTO(p posts.Post) postDTO {
	dto := toPostDTO(p)
	dto.Body = p.Body
	return dto
}

// pageDTO is the stable, public JSON shape of a page. As with postDTO it exposes
// only the contract fields; Body is populated on detail reads only.
type pageDTO struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Body        string     `json:"body,omitempty"`
	Status      string     `json:"status"`
	Template    string     `json:"template"`
	ParentID    *string    `json:"parentId"`
	PublishedAt *time.Time `json:"publishedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// toPageDTO maps a domain page onto the list DTO (no body).
func toPageDTO(p pages.Page) pageDTO {
	var parent *string
	if p.ParentID != nil {
		s := p.ParentID.String()
		parent = &s
	}
	return pageDTO{
		ID:          p.ID.String(),
		Title:       p.Title,
		Slug:        p.Slug,
		Status:      p.Status.String(),
		Template:    p.Template,
		ParentID:    parent,
		PublishedAt: p.PublishedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// toPageDetailDTO maps a domain page onto the detail DTO (full body).
func toPageDetailDTO(p pages.Page) pageDTO {
	dto := toPageDTO(p)
	dto.Body = p.Body
	return dto
}

// listResponse is the paginated list payload nested under the "data" envelope
// key: {"items":[...],"total":N,"page":P,"perPage":PP}.
type listResponse struct {
	Items   any `json:"items"`
	Total   int `json:"total"`
	Page    int `json:"page"`
	PerPage int `json:"perPage"`
}
