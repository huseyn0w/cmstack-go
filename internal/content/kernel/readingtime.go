package kernel

import (
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// wordsPerMinute is the average adult silent-reading rate used to estimate
// reading time, matching the laravel reference (words / 200).
const wordsPerMinute = 200

// stripTagsPolicy removes ALL markup, leaving plain text for word counting.
var stripTagsPolicy = bluemonday.StrictPolicy()

// ReadingTimeMinutes estimates the reading time of HTML content in whole
// minutes (words / 200, rounded up, minimum 1 for any non-empty content). Tags
// are stripped before counting so markup does not inflate the word count.
func ReadingTimeMinutes(html string) int {
	text := stripTagsPolicy.Sanitize(html)
	words := len(strings.Fields(text))
	if words == 0 {
		return 0
	}
	minutes := words / wordsPerMinute
	if words%wordsPerMinute != 0 {
		minutes++
	}
	if minutes < 1 {
		minutes = 1
	}
	return minutes
}
