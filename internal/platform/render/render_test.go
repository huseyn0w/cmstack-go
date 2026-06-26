package render

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func fragment(html string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, html)
		return err
	})
}

func TestComponentWritesHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	err := Component(context.Background(), rec, http.StatusCreated, fragment("<p>hi</p>"))
	if err != nil {
		t.Fatalf("Component: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "<p>hi</p>") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestToString(t *testing.T) {
	got, err := ToString(context.Background(), fragment("<span>x</span>"))
	if err != nil {
		t.Fatalf("ToString: %v", err)
	}
	if got != "<span>x</span>" {
		t.Errorf("got %q", got)
	}
}
