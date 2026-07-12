package templ

import (
	"context"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
)

func TestHomeRendersLayout(t *testing.T) {
	html, err := render.ToString(context.Background(), Home())
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{
		"<main",           // semantic landmark
		`id="main"`,       // skip-link target
		"Skip to content", // a11y skip link
		"/static/app.css", // compiled tailwind link
		"bg-bg",           // a design token utility (maps to --bg)
		`lang="en"`,       // language attribute
		"htmx.min.js",     // vendored htmx
		"alpine.min.js",   // vendored alpine
		"Agentic CMS-Go",      // title
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered layout missing %q", want)
		}
	}
}
