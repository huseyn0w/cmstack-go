package kernel

import "time"

// SitemapItem is the lightweight per-content-type enumeration row shared by the
// crawler-facing routes (M8): the XML sitemap (uses Slug + UpdatedAt), and the
// llms.txt / llms-full.txt Markdown indexes (use Title + Description). It
// deliberately carries NO body/heavy fields so the enumerating queries stay
// cheap. Title is the display label (meta_title with a fallback to the content
// title); Description is the short summary line (meta_description with a fallback
// to the type's excerpt/summary, empty for pages which have neither).
type SitemapItem struct {
	Slug        string
	Title       string
	Description string
	UpdatedAt   time.Time
}
