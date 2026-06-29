package recaptcha

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifier_DisabledAllowsWithoutKeys(t *testing.T) {
	v := New("", 0.5)
	if v.Enabled() {
		t.Fatal("verifier with empty secret should be disabled")
	}
	ok, err := v.Verify(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("disabled verifier must allow (graceful no-op)")
	}
}

func TestVerifier_EnabledRejectsEmptyToken(t *testing.T) {
	v := New("secret", 0.5)
	if !v.Enabled() {
		t.Fatal("verifier with a secret should be enabled")
	}
	ok, err := v.Verify(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("enabled verifier must reject an empty token")
	}
}

func TestVerifier_EnabledVerifiesAgainstServer(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"pass high score", `{"success":true,"score":0.9}`, true},
		{"reject low score", `{"success":true,"score":0.1}`, false},
		{"reject failure", `{"success":false,"score":0.9}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Fatalf("parse form: %v", err)
				}
				if r.PostFormValue("response") != "tok" {
					t.Fatalf("expected token forwarded, got %q", r.PostFormValue("response"))
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			old := verifyURL
			verifyURL = srv.URL
			defer func() { verifyURL = old }()

			v := New("secret", 0.5)
			ok, err := v.Verify(context.Background(), "tok")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tc.want {
				t.Fatalf("Verify = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestPasses(t *testing.T) {
	if !passes(siteVerifyResponse{Success: true, Score: 0.5}, 0.5) {
		t.Fatal("score at threshold should pass")
	}
	if passes(siteVerifyResponse{Success: true, Score: 0.49}, 0.5) {
		t.Fatal("score below threshold should fail")
	}
	if passes(siteVerifyResponse{Success: false, Score: 1.0}, 0.5) {
		t.Fatal("unsuccessful verification should fail regardless of score")
	}
}
