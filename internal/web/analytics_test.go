package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

func TestValidateGA4ID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"valid G-", "G-ABC1234", "G-ABC1234"},
		{"valid GT-", "GT-ABC1234", "GT-ABC1234"},
		{"valid AW-", "AW-1234567", "AW-1234567"},
		{"valid DC-", "DC-1234567", "DC-1234567"},
		{"empty", "", ""},
		{"garbage", "garbage", ""},
		{"script injection", `G-x"><script>alert(1)</script>`, ""},
		{"attr break", `"><img>`, ""},
		{"lowercase body", "G-abc1234", ""},
		{"too short", "G-ABC", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateGA4ID(tt.in); got != tt.want {
				t.Errorf("validateGA4ID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateGTMID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"valid", "GTM-ABCD12", "GTM-ABCD12"},
		{"empty", "", ""},
		{"garbage", "garbage", ""},
		{"script injection", `GTM-x"><script>alert(1)</script>`, ""},
		{"attr break", `"><img>`, ""},
		{"wrong prefix", "G-ABCD12", ""},
		{"hyphen in body", "GTM-AB-CD", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateGTMID(tt.in); got != tt.want {
				t.Errorf("validateGTMID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// fakeAnalyticsSettings is a stub AnalyticsSettings returning fixed values.
type fakeAnalyticsSettings struct {
	vals map[string]string
	err  error
}

func (f fakeAnalyticsSettings) Get(_ context.Context, key string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	v, ok := f.vals[key]
	return v, ok, nil
}

func TestAnalyticsMiddleware_StoresValidatedSnippets(t *testing.T) {
	tests := []struct {
		name    string
		vals    map[string]string
		err     error
		wantGA4 string
		wantGTM string
	}{
		{
			name:    "both valid",
			vals:    map[string]string{keyAnalyticsGA4ID: "G-ABC1234", keyAnalyticsGTMID: "GTM-ABCD12"},
			wantGA4: "G-ABC1234",
			wantGTM: "GTM-ABCD12",
		},
		{
			name:    "invalid ga4 dropped, gtm kept",
			vals:    map[string]string{keyAnalyticsGA4ID: `G-x"><script>`, keyAnalyticsGTMID: "GTM-ABCD12"},
			wantGA4: "",
			wantGTM: "GTM-ABCD12",
		},
		{
			name:    "empty settings disabled",
			vals:    map[string]string{},
			wantGA4: "",
			wantGTM: "",
		},
		{
			name:    "store error degrades to disabled",
			err:     errors.New("boom"),
			wantGA4: "",
			wantGTM: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got webtempl.AnalyticsSnippets
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = analyticsFromContext(r.Context())
			})
			mw := AnalyticsMiddleware(fakeAnalyticsSettings{vals: tt.vals, err: tt.err})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			mw(next).ServeHTTP(httptest.NewRecorder(), req)

			if got.GA4ID != tt.wantGA4 {
				t.Errorf("GA4ID = %q, want %q", got.GA4ID, tt.wantGA4)
			}
			if got.GTMID != tt.wantGTM {
				t.Errorf("GTMID = %q, want %q", got.GTMID, tt.wantGTM)
			}
		})
	}
}

func TestAnalyticsViewSource_ZeroWhenMiddlewareDidNotRun(t *testing.T) {
	got := analyticsViewSource{}.Snippets(context.Background())
	if got != (webtempl.AnalyticsSnippets{}) {
		t.Errorf("Snippets without middleware = %+v, want zero value", got)
	}
}
