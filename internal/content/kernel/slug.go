package kernel

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// maxSlugLen bounds the base slug length before any dedupe suffix so the column
// (and any future index) stays comfortably within limits.
const maxSlugLen = 200

// diacriticStripper folds a string to NFKD then drops combining marks so
// accented characters degrade to their ASCII base (é -> e) before slugging.
var diacriticStripper = transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// Slugify converts arbitrary text into a URL-friendly slug: diacritics are
// stripped, the result is lowercased, runs of non-alphanumerics collapse to a
// single hyphen, and leading/trailing hyphens are trimmed. It returns
// "untitled" when nothing usable remains, mirroring the ts implementation.
func Slugify(input string) string {
	folded, _, err := transform.String(diacriticStripper, input)
	if err != nil {
		folded = input
	}
	folded = strings.ToLower(folded)

	var b strings.Builder
	prevHyphen := false
	for _, r := range folded {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
		if b.Len() >= maxSlugLen {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "untitled"
	}
	return out
}

// SlugExistsFunc reports whether a slug is already taken by a DIFFERENT entity.
// Implementations must exclude the entity being updated (excludeID) so re-saving
// a post under its own slug does not trigger a needless -2 suffix. The empty
// uuid (via a sentinel from the caller) means "exclude nothing" (create path).
type SlugExistsFunc func(ctx context.Context, slug string) (taken bool, err error)

// UniqueSlug returns desired if free, else appends -2, -3, … until an unused
// slug is found. taken decides collisions; it MUST already account for excluding
// the current entity. The loop is bounded so a pathological data set cannot spin
// forever.
func UniqueSlug(ctx context.Context, desired string, taken SlugExistsFunc) (string, error) {
	if desired == "" {
		desired = "untitled"
	}
	candidate := desired
	for suffix := 2; suffix < 10000; suffix++ {
		exists, err := taken(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", desired, suffix)
	}
	return "", fmt.Errorf("kernel: could not derive a unique slug for %q", desired)
}
