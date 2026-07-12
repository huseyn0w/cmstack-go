package kernel_test

import (
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
)

func TestReadingTimeMinutes(t *testing.T) {
	if got := kernel.ReadingTimeMinutes(""); got != 0 {
		t.Errorf("empty content reading time = %d, want 0", got)
	}
	if got := kernel.ReadingTimeMinutes("just a few words here"); got != 1 {
		t.Errorf("short content reading time = %d, want 1", got)
	}
	// 200 words -> exactly 1 minute.
	exactly200 := strings.TrimSpace(strings.Repeat("word ", 200))
	if got := kernel.ReadingTimeMinutes(exactly200); got != 1 {
		t.Errorf("200 words reading time = %d, want 1", got)
	}
	// 201 words -> rounds up to 2.
	just201 := strings.TrimSpace(strings.Repeat("word ", 201))
	if got := kernel.ReadingTimeMinutes(just201); got != 2 {
		t.Errorf("201 words reading time = %d, want 2", got)
	}
	// 500 words -> 3 minutes (500/200 = 2.5 -> 3).
	fiveHundred := strings.TrimSpace(strings.Repeat("word ", 500))
	if got := kernel.ReadingTimeMinutes(fiveHundred); got != 3 {
		t.Errorf("500 words reading time = %d, want 3", got)
	}
}

func TestReadingTimeMinutes_IgnoresMarkup(t *testing.T) {
	html := "<p>" + strings.TrimSpace(strings.Repeat("word ", 50)) + "</p>"
	if got := kernel.ReadingTimeMinutes(html); got != 1 {
		t.Errorf("reading time with markup = %d, want 1 (markup must not inflate count)", got)
	}
}
