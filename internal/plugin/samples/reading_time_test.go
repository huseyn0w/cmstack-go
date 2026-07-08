package samples

import (
	"context"
	"strings"
	"testing"
)

func TestReadingTimeFilterPrependsNote(t *testing.T) {
	// ~450 words → ceil(450/200) = 3 minutes.
	body := "<p>" + strings.Repeat("word ", 450) + "</p>"
	out := filterReadingTime(context.Background(), body)
	got, ok := out.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", out)
	}
	if !strings.HasPrefix(got, `<p class="text-caption text-muted">3 min read</p>`) {
		t.Fatalf("expected 3 min read note prefix, got: %q", got[:min(80, len(got))])
	}
	if !strings.HasSuffix(got, body) {
		t.Fatal("expected original body preserved after the note")
	}
}

func TestReadingTimeFilterMinimumOneMinute(t *testing.T) {
	out := filterReadingTime(context.Background(), "<p>short</p>").(string)
	if !strings.HasPrefix(out, `<p class="text-caption text-muted">1 min read</p>`) {
		t.Fatalf("expected minimum 1 min read, got: %q", out)
	}
}

func TestReadingTimeFilterNonStringPassthrough(t *testing.T) {
	in := 42
	out := filterReadingTime(context.Background(), in)
	if got, ok := out.(int); !ok || got != 42 {
		t.Fatalf("expected non-string passthrough of 42, got %v", out)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
