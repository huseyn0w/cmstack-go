package storage_test

import (
	"context"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/storage"
)

func TestNew_LocalIsDefault(t *testing.T) {
	for _, driver := range []string{"", "local", "LOCAL"} {
		s, handler, prefix, err := storage.New(context.Background(), storage.DriverConfig{
			Driver:            driver,
			LocalBaseDir:      t.TempDir(),
			LocalPublicPrefix: "/uploads",
		})
		if err != nil {
			t.Fatalf("driver %q: %v", driver, err)
		}
		if s == nil || handler == nil {
			t.Fatalf("driver %q: nil storage/handler", driver)
		}
		if prefix != "/uploads" {
			t.Errorf("driver %q: prefix = %q", driver, prefix)
		}
	}
}

func TestNew_S3DriverBuilds(t *testing.T) {
	s, handler, prefix, err := storage.New(context.Background(), storage.DriverConfig{
		Driver: "s3",
		S3:     storage.S3Config{Bucket: "media", Region: "us-east-1"},
	})
	if err != nil {
		t.Fatalf("s3 driver: %v", err)
	}
	if s == nil {
		t.Fatal("nil s3 storage")
	}
	// S3 objects are served by S3/CDN, not an app handler.
	if handler != nil || prefix != "" {
		t.Errorf("s3 should have no local handler/prefix, got handler=%v prefix=%q", handler != nil, prefix)
	}
}

func TestNew_UnknownDriverFails(t *testing.T) {
	if _, _, _, err := storage.New(context.Background(), storage.DriverConfig{Driver: "ftp"}); err == nil {
		t.Fatal("unknown driver should fail fast")
	}
}
