package kernel_test

import (
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
)

func TestParseStatus(t *testing.T) {
	cases := map[string]kernel.Status{
		"PUBLISHED": kernel.StatusPublished,
		"DRAFT":     kernel.StatusDraft,
		"":          kernel.StatusDraft,
		"garbage":   kernel.StatusDraft, // unrecognized never publishes
		"published": kernel.StatusDraft, // case-sensitive: lowercase is not PUBLISHED
	}
	for in, want := range cases {
		if got := kernel.ParseStatus(in); got != want {
			t.Errorf("ParseStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatusValid(t *testing.T) {
	if !kernel.StatusDraft.Valid() || !kernel.StatusPublished.Valid() {
		t.Error("canonical statuses must be valid")
	}
	if kernel.Status("nope").Valid() {
		t.Error("unknown status must be invalid")
	}
}
