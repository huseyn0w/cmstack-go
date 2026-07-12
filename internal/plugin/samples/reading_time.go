// Package samples holds the bundled first-party sample plugins that demonstrate
// the plugin hook mechanisms (actions/filters/regions) without adding required
// product behavior.
package samples

import (
	"context"
	"strconv"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/plugin"
)

// ReadingTime is a sample plugin that prepends an estimated reading-time note to
// a post's rendered HTML body via the "post_content" filter. It is disabled by
// default: posts already surface reading time natively, so this exists to
// demonstrate the filter mechanism rather than to duplicate that display.
type ReadingTime struct{}

// Meta returns the plugin catalogue metadata.
func (ReadingTime) Meta() plugin.Meta {
	return plugin.Meta{
		ID:             "reading-time",
		Name:           "Reading Time",
		Description:    "Prepends an estimated reading time to post content.",
		DefaultEnabled: false,
	}
}

// Register wires the "post_content" filter that prepends the reading-time note.
func (ReadingTime) Register(h *plugin.Hooks) {
	h.AddFilter("post_content", filterReadingTime)
}

// filterReadingTime prepends a reading-time note to a string HTML body. Non-string
// values pass through unchanged so the filter is safe if the hook is ever fed a
// different payload.
func filterReadingTime(_ context.Context, value any) any {
	body, ok := value.(string)
	if !ok {
		return value
	}
	minutes := kernel.ReadingTimeMinutes(body)
	if minutes < 1 {
		minutes = 1
	}
	note := `<p class="text-caption text-muted">` + strconv.Itoa(minutes) + ` min read</p>`
	return note + body
}
