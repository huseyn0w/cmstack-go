// Package render renders a-h/templ components to HTTP responses.
package render

import (
	"bytes"
	"context"
	"net/http"

	"github.com/a-h/templ"
)

// Component renders an a-h/templ component to w with the given status code and
// the text/html content type. The component is rendered into a buffer first so
// a mid-render error does not produce a partially written, wrong-status page.
func Component(ctx context.Context, w http.ResponseWriter, status int, c templ.Component) error {
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// ToString renders a component to a string. It is a test helper that keeps
// component assertions terse.
func ToString(ctx context.Context, c templ.Component) (string, error) {
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}
