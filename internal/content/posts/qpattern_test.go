package posts

import "testing"

func TestQPattern(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // "" means nil
	}{
		{"empty is nil", "", ""},
		{"plain term wrapped", "go", "%go%"},
		{"percent escaped", "50%", "%50\\%%"},
		{"underscore escaped", "a_b", "%a\\_b%"},
		{"backslash escaped", "a\\b", "%a\\\\b%"},
		{"mixed metacharacters", "%_\\", "%\\%\\_\\\\%"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := qPattern(c.in)
			if c.want == "" {
				if got != nil {
					t.Fatalf("qPattern(%q) = %q, want nil", c.in, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("qPattern(%q) = nil, want %q", c.in, c.want)
			}
			if *got != c.want {
				t.Errorf("qPattern(%q) = %q, want %q", c.in, *got, c.want)
			}
		})
	}
}
