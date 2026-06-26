package db

import (
	"context"
	"testing"
)

func TestNewPoolInvalidDSN(t *testing.T) {
	_, err := NewPool(context.Background(), "://not-a-dsn")
	if err == nil {
		t.Fatal("expected error for invalid DSN")
	}
}

func TestNewPoolLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live DB test in -short mode")
	}
	// Intentionally no live connection attempt here: the package must build
	// and unit-test without a database. Live connectivity is exercised by the
	// integration suite (testcontainers) gated outside -short.
	t.Skip("live DB integration covered by testcontainers suite (not run in M0 unit tests)")
}
