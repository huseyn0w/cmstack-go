package kernel_test

import (
	"strings"
	"testing"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
)

func TestSanitizeRichText_StripsXSSVectors(t *testing.T) {
	vectors := []struct {
		name string
		in   string
		// mustNotContain are substrings that must be absent from the output.
		mustNotContain []string
	}{
		{
			name:           "script tag",
			in:             `<p>hi</p><script>alert(1)</script>`,
			mustNotContain: []string{"<script", "alert(1)"},
		},
		{
			name:           "svg payload",
			in:             `<svg onload="alert(1)"><rect/></svg>`,
			mustNotContain: []string{"<svg", "onload"},
		},
		{
			name:           "javascript href",
			in:             `<a href="javascript:alert(1)">x</a>`,
			mustNotContain: []string{"javascript:"},
		},
		{
			name:           "onclick handler",
			in:             `<p onclick="steal()">click</p>`,
			mustNotContain: []string{"onclick", "steal()"},
		},
		{
			name:           "img onerror",
			in:             `<img src="x" onerror="alert(1)">`,
			mustNotContain: []string{"onerror"},
		},
		{
			name:           "data uri svg image",
			in:             `<img src="data:image/svg+xml,<svg onload=alert(1)>">`,
			mustNotContain: []string{"data:image/svg", "onload"},
		},
		{
			name:           "iframe",
			in:             `<iframe src="evil"></iframe>`,
			mustNotContain: []string{"<iframe"},
		},
		{
			name:           "style expression",
			in:             `<p style="background:url(javascript:alert(1))">x</p>`,
			mustNotContain: []string{"javascript:", "style="},
		},
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			out := kernel.SanitizeRichText(v.in)
			for _, bad := range v.mustNotContain {
				if strings.Contains(out, bad) {
					t.Errorf("output %q still contains forbidden %q", out, bad)
				}
			}
		})
	}
}

func TestSanitizeRichText_KeepsEditorialTags(t *testing.T) {
	in := `<h2>Title</h2><p>A <strong>bold</strong> <em>idea</em> with a ` +
		`<a href="https://example.com">link</a>.</p>` +
		`<ul><li>one</li><li>two</li></ul>` +
		`<blockquote>quote</blockquote><pre><code>code</code></pre>` +
		`<img src="https://cdn.example.com/x.png" alt="pic">`
	out := kernel.SanitizeRichText(in)
	for _, want := range []string{
		"<h2>", "<strong>", "<em>", "<ul>", "<li>", "<blockquote>",
		"<pre>", "<code>", "<img", `href="https://example.com"`,
		`src="https://cdn.example.com/x.png"`, `alt="pic"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output dropped allowed markup %q\ngot: %s", want, out)
		}
	}
}

func TestSanitizeRichText_AddsSafeRelToLinks(t *testing.T) {
	out := kernel.SanitizeRichText(`<a href="https://example.com">x</a>`)
	if !strings.Contains(out, "nofollow") {
		t.Errorf("link missing nofollow rel: %s", out)
	}
}

func TestSanitizeRichText_KeepsImageDimensionsAndLazyLoading(t *testing.T) {
	in := `<img src="https://cdn.example.com/x.png" alt="pic" width="800" height="600" loading="lazy">`
	out := kernel.SanitizeRichText(in)
	for _, want := range []string{`width="800"`, `height="600"`, `loading="lazy"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output dropped allowed img attr %q\ngot: %s", want, out)
		}
	}
}

func TestSanitizeRichText_StripsMaliciousImageAttrs(t *testing.T) {
	// Non-integer dimensions and non-keyword loading values must be dropped, and
	// an onerror handler must never survive.
	in := `<img src="https://cdn.example.com/x.png" alt="p" width="1&quot;onerror=alert(1)" ` +
		`height="junk" loading="javascript:alert(1)" onerror="alert(1)">`
	out := kernel.SanitizeRichText(in)
	for _, bad := range []string{"onerror", "javascript:", "junk", "alert(1)"} {
		if strings.Contains(out, bad) {
			t.Errorf("output kept dangerous token %q\ngot: %s", bad, out)
		}
	}
}
