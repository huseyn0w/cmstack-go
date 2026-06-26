package health

import (
	"context"
	"errors"
	"testing"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestServiceLive(t *testing.T) {
	s := NewService(nil)
	if got := s.Live(); got.Status != StatusOK {
		t.Errorf("Live() = %q, want %q", got.Status, StatusOK)
	}
}

func TestServiceReady(t *testing.T) {
	tests := []struct {
		name   string
		db     Pinger
		want   string
		detail bool
	}{
		{name: "nil db is unavailable", db: nil, want: StatusUnavailable, detail: true},
		{name: "healthy db is ok", db: fakePinger{err: nil}, want: StatusOK},
		{name: "failing db is unavailable", db: fakePinger{err: errors.New("down")}, want: StatusUnavailable, detail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService(tt.db)
			got := s.Ready(context.Background())
			if got.Status != tt.want {
				t.Errorf("Ready() status = %q, want %q", got.Status, tt.want)
			}
			if tt.detail && got.Detail == "" {
				t.Error("expected a detail message")
			}
		})
	}
}
