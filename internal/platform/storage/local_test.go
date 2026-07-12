package storage_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/storage"
)

func newLocal(t *testing.T) *storage.LocalStorage {
	t.Helper()
	s, err := storage.NewLocalStorage(t.TempDir(), "/uploads")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}
	return s
}

func TestLocalStorage_SaveServeDelete(t *testing.T) {
	s := newLocal(t)
	ctx := context.Background()
	key := "avatars/u1/abc.png"

	if _, err := s.Save(ctx, key, bytes.NewReader(pngMagic), "image/png"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := s.URL(key); got != "/uploads/"+key {
		t.Errorf("URL = %q", got)
	}

	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/uploads/" + key)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("missing nosniff header, got %q", got)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Deleting again is a no-op.
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("second Delete should be nil, got %v", err)
	}
	resp2, err := http.Get(srv.URL + "/uploads/" + key)
	if err != nil {
		t.Fatalf("GET after delete: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp2.StatusCode)
	}
}

func TestLocalStorage_NeutralizesTraversalKey(t *testing.T) {
	// A "../" key must never escape the base dir. safePath collapses it to a path
	// inside the root rather than writing outside, so the write is contained.
	s := newLocal(t)
	if _, err := s.Save(context.Background(), "../escape.txt", strings.NewReader("x"), "text/plain"); err != nil {
		t.Fatalf("contained traversal write should succeed inside root, got %v", err)
	}
	// Serving it back must resolve within the root, proving containment. Go's http
	// client normalizes the URL, so the file is reachable at its collapsed path;
	// the key never reached outside the root, which is the point.
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/uploads/escape.txt")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("contained file should be served, got %d", resp.StatusCode)
	}
}

func TestLocalStorage_HandlerRejectsTraversal(t *testing.T) {
	s := newLocal(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	// Encoded traversal attempt; the handler must not serve outside base dir.
	resp, err := http.Get(srv.URL + "/uploads/..%2f..%2fetc%2fpasswd")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("traversal should not return 200, got %d", resp.StatusCode)
	}
}

func TestLocalStorage_EmptyDirRejected(t *testing.T) {
	if _, err := storage.NewLocalStorage("", "/uploads"); err == nil {
		t.Fatal("expected error for empty base dir")
	}
}
