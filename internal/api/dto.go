package api

import (
	"encoding/json"
	"time"

	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/comments"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/media"
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
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

// categoryDTO is the stable, public JSON shape of a taxonomy category. It is a
// flat projection (the tree is not exposed here); parentId is null for a root.
type categoryDTO struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	ParentID    *string   `json:"parentId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// toCategoryDTO maps a domain category onto its DTO.
func toCategoryDTO(c categories.Category) categoryDTO {
	var parent *string
	if c.ParentID != nil {
		s := c.ParentID.String()
		parent = &s
	}
	return categoryDTO{
		ID:          c.ID.String(),
		Name:        c.Name,
		Slug:        c.Slug,
		Description: c.Description,
		ParentID:    parent,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

// tagDTO is the stable, public JSON shape of a taxonomy tag.
type tagDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// toTagDTO maps a domain tag onto its DTO.
func toTagDTO(t tags.Tag) tagDTO {
	return tagDTO{
		ID:        t.ID.String(),
		Name:      t.Name,
		Slug:      t.Slug,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// mediaDTO is the stable, public JSON shape of an uploaded asset. It exposes the
// display + rendering metadata and the resolved URL; the internal storage key is
// intentionally omitted (callers use url). Width/Height are null for documents.
type mediaDTO struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	MIME      string    `json:"mime"`
	Size      int64     `json:"size"`
	Width     *int      `json:"width"`
	Height    *int      `json:"height"`
	Alt       string    `json:"alt"`
	Title     string    `json:"title"`
	Caption   string    `json:"caption"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
}

// toMediaDTO maps a domain media asset onto its DTO. url is resolved via the
// supplied resolver (the storage backend's public URL for the object key).
func toMediaDTO(m media.Media, url func(key string) string) mediaDTO {
	return mediaDTO{
		ID:        m.ID.String(),
		Filename:  m.OriginalFilename,
		MIME:      m.MIME,
		Size:      m.SizeBytes,
		Width:     m.Width,
		Height:    m.Height,
		Alt:       m.Alt,
		Title:     m.Title,
		Caption:   m.Caption,
		URL:       url(m.StorageKey),
		CreatedAt: m.CreatedAt,
	}
}

// commentDTO is the stable moderation JSON shape of a comment. It DELIBERATELY
// omits the author IP (PII); the email is included because this is the gated,
// moderator-only surface (mirrors the admin moderation table, not the public
// thread).
type commentDTO struct {
	ID          string     `json:"id"`
	PostID      string     `json:"postId"`
	ParentID    *string    `json:"parentId"`
	AuthorName  string     `json:"authorName"`
	AuthorEmail string     `json:"authorEmail"`
	Body        string     `json:"body"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	EditedAt    *time.Time `json:"editedAt"`
}

// toCommentDTO maps a domain comment onto its moderation DTO. AuthorIP is never
// copied — the DTO must not leak the commenter's IP address.
func toCommentDTO(c comments.Comment) commentDTO {
	var parent *string
	if c.ParentID != nil {
		s := c.ParentID.String()
		parent = &s
	}
	return commentDTO{
		ID:          c.ID.String(),
		PostID:      c.PostID.String(),
		ParentID:    parent,
		AuthorName:  c.AuthorName,
		AuthorEmail: c.AuthorEmail,
		Body:        c.Body,
		Status:      c.Status.String(),
		CreatedAt:   c.CreatedAt,
		EditedAt:    c.EditedAt,
	}
}

// revisionDTO is the stable JSON shape of a content revision. It exposes the
// scalar snapshot summary (title/excerpt) but never the full body — the list is
// a history index, not a body dump.
type revisionDTO struct {
	ID        string    `json:"id"`
	AuthorID  *string   `json:"authorId"`
	CreatedAt time.Time `json:"createdAt"`
	Title     string    `json:"title"`
	Excerpt   string    `json:"excerpt"`
}

// revisionSnapshot is the subset of the opaque revision snapshot the DTO reads.
// The post/page snapshots both carry title + (posts only) excerpt; a missing
// field simply decodes to "".
type revisionSnapshot struct {
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
}

// toRevisionDTO maps a kernel.Revision onto its DTO, decoding the opaque
// snapshot for the title/excerpt summary. The full body is never surfaced.
func toRevisionDTO(rev kernel.Revision) revisionDTO {
	var snap revisionSnapshot
	_ = json.Unmarshal(rev.Snapshot, &snap)
	var author *string
	if rev.AuthorID != nil {
		s := rev.AuthorID.String()
		author = &s
	}
	return revisionDTO{
		ID:        rev.ID.String(),
		AuthorID:  author,
		CreatedAt: rev.CreatedAt,
		Title:     snap.Title,
		Excerpt:   snap.Excerpt,
	}
}
