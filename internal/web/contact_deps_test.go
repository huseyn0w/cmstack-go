package web

import (
	"context"
	"errors"
	"testing"
)

// stubSettings is a controllable contactSettingsReader.
type stubSettings struct {
	value string
	ok    bool
	err   error
}

func (s stubSettings) Get(context.Context, string) (string, bool, error) {
	return s.value, s.ok, s.err
}

func TestContactRecipientResolver_Precedence(t *testing.T) {
	cases := []struct {
		name     string
		settings contactSettingsReader
		cfg      string
		admin    string
		want     string
	}{
		{"settings wins", stubSettings{value: "s@x.com", ok: true}, "c@x.com", "a@x.com", "s@x.com"},
		{"empty settings falls to config", stubSettings{value: "  ", ok: true}, "c@x.com", "a@x.com", "c@x.com"},
		{"unset settings falls to config", stubSettings{ok: false}, "c@x.com", "a@x.com", "c@x.com"},
		{"settings error falls to config", stubSettings{err: errors.New("db")}, "c@x.com", "a@x.com", "c@x.com"},
		{"config empty falls to admin", stubSettings{ok: false}, "", "a@x.com", "a@x.com"},
		{"nil settings uses config", nil, "c@x.com", "a@x.com", "c@x.com"},
		{"everything empty yields empty", stubSettings{ok: false}, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewContactRecipientResolver(tc.settings, tc.cfg, tc.admin)
			if got := r.Recipient(context.Background()); got != tc.want {
				t.Fatalf("Recipient = %q, want %q", got, tc.want)
			}
		})
	}
}
