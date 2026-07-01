package i18n

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in     string
		want   Locale
		wantOK bool
	}{
		{"en", LocaleEN, true},
		{"de", LocaleDE, true},
		{"ru", LocaleRU, true},
		{"DE", LocaleDE, true},        // case-insensitive
		{"de-DE", LocaleDE, true},     // region suffix stripped
		{"ru_RU", LocaleRU, true},     // underscore suffix stripped
		{"  en  ", LocaleEN, true},    // trimmed
		{"fr", Default(), false},      // unsupported -> default, not ok
		{"", Default(), false},        // empty -> default, not ok
		{"english", Default(), false}, // not a base tag
	}
	for _, c := range cases {
		got, ok := Parse(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("Parse(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestIsSupportedAndDefault(t *testing.T) {
	if Default() != LocaleEN {
		t.Fatalf("Default() = %q, want en", Default())
	}
	if !LocaleEN.IsDefault() {
		t.Error("en should be default")
	}
	if LocaleDE.IsDefault() {
		t.Error("de should not be default")
	}
	for _, l := range []Locale{LocaleEN, LocaleDE, LocaleRU} {
		if !IsSupported(l) {
			t.Errorf("IsSupported(%q) = false", l)
		}
	}
	if IsSupported(Locale("fr")) {
		t.Error("fr should not be supported")
	}
}

func TestAllOrderAndCopy(t *testing.T) {
	all := All()
	want := []Locale{LocaleEN, LocaleDE, LocaleRU}
	if len(all) != len(want) {
		t.Fatalf("All() len = %d, want %d", len(all), len(want))
	}
	for i := range want {
		if all[i] != want[i] {
			t.Errorf("All()[%d] = %q, want %q", i, all[i], want[i])
		}
	}
	// Mutating the returned slice must not affect the package state.
	all[0] = LocaleRU
	if All()[0] != LocaleEN {
		t.Error("All() returned a shared slice; mutation leaked")
	}
}
