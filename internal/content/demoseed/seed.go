// Package demoseed idempotently seeds demo CONTENT (posts + pages) with per-
// locale translations so a fresh install has something to show. The dataset is
// the SAME canonical demo-content used by the reference stacks (6 posts, 2
// pages, each with en/de/ru title/excerpt/content), embedded as JSON.
//
// Storage follows the base-row + overlay pattern (M7b): the en content lives on
// the base posts/pages row; de/ru live as overlay rows in post_translations /
// page_translations. Everything is keyed by slug for idempotency — re-running
// updates existing rows in place and never creates duplicates.
package demoseed

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

//go:embed demo-content-i18n.json
var demoContentJSON []byte

// localized is a per-locale string map (keys "en"/"de"/"ru").
type localized map[string]string

// postSeed is one demo post from the dataset.
type postSeed struct {
	Slug         string    `json:"slug"`
	CategorySlug string    `json:"categorySlug"`
	TagSlugs     []string  `json:"tagSlugs"`
	Title        localized `json:"title"`
	Excerpt      localized `json:"excerpt"`
	Content      localized `json:"content"`
}

// pageSeed is one demo page from the dataset.
type pageSeed struct {
	Slug    string    `json:"slug"`
	Title   localized `json:"title"`
	Content localized `json:"content"`
}

// dataset is the embedded demo-content document (posts + pages are what this
// seeder inserts; categories/tags are carried for parity but not required by the
// Go schema seams here).
type dataset struct {
	Locales []string   `json:"locales"`
	Posts   []postSeed `json:"posts"`
	Pages   []pageSeed `json:"pages"`
}

// Seeder inserts the demo content within a single transaction. It needs a tx
// beginner, the sqlc queries, and the id of the author to attribute posts to.
type Seeder struct {
	pool db.Beginner
	q    *sqlcgen.Queries
}

// NewSeeder constructs a demo-content Seeder.
func NewSeeder(pool db.Beginner, q *sqlcgen.Queries) *Seeder {
	return &Seeder{pool: pool, q: q}
}

// Result reports how much content the seeder ensured exists.
type Result struct {
	PostsCreated int
	PostsUpdated int
	PagesCreated int
	PagesUpdated int
	Locales      []string
}

// Seed ensures every demo post and page exists (base en row + de/ru overlays),
// attributed to authorID and PUBLISHED. It is idempotent: keyed by slug, a
// re-run updates the existing rows in place. Everything runs in one transaction.
func (s *Seeder) Seed(ctx context.Context, authorID pgtype.UUID) (Result, error) {
	var data dataset
	if err := json.Unmarshal(demoContentJSON, &data); err != nil {
		return Result{}, fmt.Errorf("demoseed: parse dataset: %w", err)
	}

	res := Result{Locales: data.Locales}
	err := db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		for _, p := range data.Posts {
			created, err := seedPost(ctx, tx, q, p, authorID)
			if err != nil {
				return fmt.Errorf("seed post %q: %w", p.Slug, err)
			}
			if created {
				res.PostsCreated++
			} else {
				res.PostsUpdated++
			}
		}
		for _, pg := range data.Pages {
			created, err := seedPage(ctx, tx, q, pg)
			if err != nil {
				return fmt.Errorf("seed page %q: %w", pg.Slug, err)
			}
			if created {
				res.PagesCreated++
			} else {
				res.PagesUpdated++
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

const publishedStatus = "PUBLISHED"

// seedPost ensures the base post row (en content) exists and refreshes the
// de/ru overlay rows. Returns whether the base row was newly created.
func seedPost(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, p postSeed, authorID pgtype.UUID) (bool, error) {
	postID, existed, err := existingID(ctx, tx, "posts", p.Slug)
	if err != nil {
		return false, err
	}

	if !existed {
		row, err := q.CreatePost(ctx, sqlcgen.CreatePostParams{
			Title:       p.Title[i18n.Default().String()],
			Slug:        p.Slug,
			Excerpt:     p.Excerpt[i18n.Default().String()],
			Body:        p.Content[i18n.Default().String()],
			Status:      publishedStatus,
			PublishedAt: pgtype.Timestamptz{Time: nowUTC(), Valid: true},
			AuthorID:    authorID,
		})
		if err != nil {
			return false, err
		}
		postID = row.ID
	} else {
		// Refresh the base (en) content + keep it published/attributed.
		if _, err := tx.Exec(ctx,
			`UPDATE posts SET title=$2, excerpt=$3, body=$4, status=$5, author_id=$6,
			     published_at=COALESCE(published_at, now()), updated_at=now()
			 WHERE id=$1`,
			postID, p.Title[i18n.Default().String()], p.Excerpt[i18n.Default().String()],
			p.Content[i18n.Default().String()], publishedStatus, authorID,
		); err != nil {
			return false, err
		}
	}

	// Overlay rows for every NON-default locale that has content.
	for _, loc := range i18n.All() {
		if loc == i18n.Default() {
			continue
		}
		l := loc.String()
		if _, err := q.UpsertPostTranslation(ctx, sqlcgen.UpsertPostTranslationParams{
			PostID:  postID,
			Locale:  l,
			Title:   p.Title[l],
			Excerpt: p.Excerpt[l],
			Body:    p.Content[l],
		}); err != nil {
			return false, err
		}
	}
	return !existed, nil
}

// seedPage ensures the base page row (en content) exists and refreshes the
// de/ru overlay rows. Returns whether the base row was newly created.
func seedPage(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, pg pageSeed) (bool, error) {
	pageID, existed, err := existingID(ctx, tx, "pages", pg.Slug)
	if err != nil {
		return false, err
	}

	if !existed {
		row, err := q.CreatePage(ctx, sqlcgen.CreatePageParams{
			Title:       pg.Title[i18n.Default().String()],
			Slug:        pg.Slug,
			Body:        pg.Content[i18n.Default().String()],
			Status:      publishedStatus,
			PublishedAt: pgtype.Timestamptz{Time: nowUTC(), Valid: true},
			Template:    "default",
		})
		if err != nil {
			return false, err
		}
		pageID = row.ID
	} else {
		if _, err := tx.Exec(ctx,
			`UPDATE pages SET title=$2, body=$3, status=$4,
			     published_at=COALESCE(published_at, now()), updated_at=now()
			 WHERE id=$1`,
			pageID, pg.Title[i18n.Default().String()], pg.Content[i18n.Default().String()], publishedStatus,
		); err != nil {
			return false, err
		}
	}

	for _, loc := range i18n.All() {
		if loc == i18n.Default() {
			continue
		}
		l := loc.String()
		if _, err := q.UpsertPageTranslation(ctx, sqlcgen.UpsertPageTranslationParams{
			PageID: pageID,
			Locale: l,
			Title:  pg.Title[l],
			Body:   pg.Content[l],
		}); err != nil {
			return false, err
		}
	}
	return !existed, nil
}

// existingID looks up a row id by slug in the given table (any status). The
// table name is a package constant ("posts"/"pages"), never user input, so the
// fmt-built query is safe.
func existingID(ctx context.Context, tx pgx.Tx, table, slug string) (pgtype.UUID, bool, error) {
	var id pgtype.UUID
	err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE slug=$1`, table), slug).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, false, nil
		}
		return pgtype.UUID{}, false, err
	}
	return id, true, nil
}

// nowUTC returns the current time in UTC, used for published_at stamps.
func nowUTC() time.Time { return time.Now().UTC() }
