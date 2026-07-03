package theme

import "testing"

// TestResolveFallback asserts an empty or unknown id resolves to the default
// theme, while a registered id resolves to itself.
func TestResolveFallback(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want string
	}{
		{"empty falls back to default", "", "default"},
		{"unknown falls back to default", "does-not-exist", "default"},
		{"hostile falls back to default", "../etc/passwd", "default"},
		{"default resolves to itself", "default", "default"},
		{"sepia resolves to itself", "sepia", "sepia"},
		{"noir resolves to itself", "noir", "noir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.id).ID; got != tc.want {
				t.Errorf("Resolve(%q).ID = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

// TestAllOrdering asserts All() returns the default theme first and includes the
// alternates, with a defensive copy (mutating the result must not affect the
// registry).
func TestAllOrdering(t *testing.T) {
	all := All()
	if len(all) < 3 {
		t.Fatalf("All() returned %d themes, want at least 3", len(all))
	}
	if all[0].ID != "default" {
		t.Errorf("All()[0].ID = %q, want default first", all[0].ID)
	}

	ids := map[string]bool{}
	for _, th := range all {
		if th.ID == "" || th.Label == "" || th.Description == "" {
			t.Errorf("theme %+v has an empty required field", th)
		}
		ids[th.ID] = true
	}
	for _, want := range []string{"default", "sepia", "noir"} {
		if !ids[want] {
			t.Errorf("All() missing theme %q", want)
		}
	}

	// Defensive copy: mutating the returned slice must not corrupt the registry.
	all[0].ID = "mutated"
	if Default().ID != "default" {
		t.Fatalf("mutating All() result corrupted the registry: Default().ID = %q", Default().ID)
	}
}

// TestIsValidAndGet asserts IsValid + Get agree with the registry.
func TestIsValidAndGet(t *testing.T) {
	if !IsValid("sepia") {
		t.Error("IsValid(sepia) = false, want true")
	}
	if IsValid("nope") {
		t.Error("IsValid(nope) = true, want false")
	}
	if _, ok := Get("noir"); !ok {
		t.Error("Get(noir) not found")
	}
	if _, ok := Get("nope"); ok {
		t.Error("Get(nope) found, want not found")
	}
}

// TestDefault asserts Default() is the base "default" theme.
func TestDefault(t *testing.T) {
	if Default().ID != "default" {
		t.Errorf("Default().ID = %q, want default", Default().ID)
	}
}
