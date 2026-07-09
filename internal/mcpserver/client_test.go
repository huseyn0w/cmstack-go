package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// recordedRequest captures what the httptest server received.
type recordedRequest struct {
	method string
	path   string
	query  string
	auth   string
	ctype  string
	body   string
}

// newRecordingServer returns an httptest.Server that records the last request
// and replies with status + body.
func newRecordingServer(t *testing.T, status int, respBody string) (*httptest.Server, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		rec.auth = r.Header.Get("Authorization")
		rec.ctype = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		rec.body = string(b)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestDoGetWithQueryUnwrapsData(t *testing.T) {
	srv, rec := newRecordingServer(t, http.StatusOK, `{"data":{"items":[1,2],"total":2}}`)
	c := New(srv.URL, "tok-123", srv.Client())

	q := url.Values{}
	q.Set("status", "PUBLISHED")
	q.Set("perPage", "10")
	raw, err := c.do(context.Background(), "GET", "/posts", q, nil)
	if err != nil {
		t.Fatalf("do: %v", err)
	}

	if rec.method != "GET" {
		t.Errorf("method = %q, want GET", rec.method)
	}
	if rec.path != "/api/v1/posts" {
		t.Errorf("path = %q, want /api/v1/posts", rec.path)
	}
	if rec.auth != "Bearer tok-123" {
		t.Errorf("auth = %q, want Bearer tok-123", rec.auth)
	}
	if got := rec.query; got != "perPage=10&status=PUBLISHED" {
		t.Errorf("query = %q", got)
	}

	var data struct {
		Items []int `json:"items"`
		Total int   `json:"total"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data.Total != 2 || len(data.Items) != 2 {
		t.Errorf("unwrapped data = %+v", data)
	}
}

func TestDoPostWithBodySetsJSONContentType(t *testing.T) {
	srv, rec := newRecordingServer(t, http.StatusCreated, `{"data":{"id":"p1","title":"Hi"}}`)
	c := New(srv.URL, "tok", srv.Client())

	raw, err := c.do(context.Background(), "POST", "/posts", nil, map[string]string{"title": "Hi"})
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if rec.method != "POST" {
		t.Errorf("method = %q, want POST", rec.method)
	}
	if rec.ctype != "application/json" {
		t.Errorf("content-type = %q, want application/json", rec.ctype)
	}
	if rec.body != `{"title":"Hi"}` {
		t.Errorf("body = %q", rec.body)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["id"] != "p1" {
		t.Errorf("data = %+v", got)
	}
}

func TestDoDeleteNoContent(t *testing.T) {
	srv, rec := newRecordingServer(t, http.StatusNoContent, "")
	c := New(srv.URL, "tok", srv.Client())

	raw, err := c.do(context.Background(), "DELETE", "/posts/xyz", nil, nil)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if raw != nil {
		t.Errorf("expected nil raw for 204, got %q", raw)
	}
	if rec.method != "DELETE" || rec.path != "/api/v1/posts/xyz" {
		t.Errorf("method/path = %q %q", rec.method, rec.path)
	}
}

func TestDoMapsErrorEnvelope(t *testing.T) {
	srv, _ := newRecordingServer(t, http.StatusForbidden, `{"error":{"code":"forbidden","message":"no permission"}}`)
	c := New(srv.URL, "tok", srv.Client())

	_, err := c.do(context.Background(), "GET", "/posts", nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.Status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", apiErr.Status)
	}
	if apiErr.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", apiErr.Code)
	}
	if apiErr.Message != "no permission" {
		t.Errorf("message = %q, want 'no permission'", apiErr.Message)
	}
}

func TestDoMapsNonEnvelopeErrorBody(t *testing.T) {
	srv, _ := newRecordingServer(t, http.StatusBadGateway, "upstream boom")
	c := New(srv.URL, "tok", srv.Client())

	_, err := c.do(context.Background(), "GET", "/posts", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.Status != http.StatusBadGateway || apiErr.Message != "upstream boom" {
		t.Errorf("apiErr = %+v", apiErr)
	}
}
