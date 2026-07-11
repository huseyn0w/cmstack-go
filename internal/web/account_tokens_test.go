package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/apitoken"
)

// fakeAPITokenService is a controllable APITokenService for the account tokens
// handler tests.
type fakeAPITokenService struct {
	tokens  []apitoken.Token
	listErr error

	genErr       error
	genPlaintext string
	genTok       apitoken.Token
	genCalled    bool
	genUserID    uuid.UUID
	genName      string
	genExpiresAt *time.Time

	revokeErr    error
	revokeCalled bool
	revokeID     uuid.UUID
	revokeUserID uuid.UUID
}

func (f *fakeAPITokenService) List(_ context.Context, _ uuid.UUID) ([]apitoken.Token, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tokens, nil
}

func (f *fakeAPITokenService) Generate(_ context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (string, apitoken.Token, error) {
	f.genCalled = true
	f.genUserID = userID
	f.genName = name
	f.genExpiresAt = expiresAt
	if f.genErr != nil {
		return "", apitoken.Token{}, f.genErr
	}
	return f.genPlaintext, f.genTok, nil
}

func (f *fakeAPITokenService) Revoke(_ context.Context, id, userID uuid.UUID) error {
	f.revokeCalled = true
	f.revokeID = id
	f.revokeUserID = userID
	return f.revokeErr
}

func testTokensShell() adminShellDeps {
	return adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{role: accounts.Role{Label: "Author"}}, csrf: func(*http.Request) string { return "x" }, siteURL: "https://site.test"}
}

func TestAccountTokensList_ShowsOnlyCurrentUsersTokensWithStatuses(t *testing.T) {
	u := authedUser(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)
	revokedAt := now.Add(-time.Hour)

	svc := &fakeAPITokenService{tokens: []apitoken.Token{
		{ID: uuid.New(), UserID: u.ID, Name: "active-tok", LastFour: "abcd", CreatedAt: now},
		{ID: uuid.New(), UserID: u.ID, Name: "revoked-tok", LastFour: "wxyz", CreatedAt: now, RevokedAt: &revokedAt},
		{ID: uuid.New(), UserID: u.ID, Name: "expired-tok", LastFour: "1234", CreatedAt: now, ExpiresAt: &past},
		{ID: uuid.New(), UserID: u.ID, Name: "future-tok", LastFour: "5678", CreatedAt: now, ExpiresAt: &future},
	}}

	h := NewAccountTokensHandler(svc, testTokensShell(), func(*http.Request) string { return "csrf-tok" })
	h.now = func() time.Time { return now }

	req := httptest.NewRequest(http.MethodGet, "/account/tokens", nil)
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"active-tok", "revoked-tok", "expired-tok", "future-tok", "…abcd", "…wxyz", "…1234", "…5678"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in body", want)
		}
	}
	// No secret material (hash/plaintext) should ever be rendered.
	if strings.Contains(body, "TokenHash") {
		t.Error("must not render token hash field name")
	}
}

func TestAccountTokensCreate_ValidNameCallsGenerateAndRevealsPlaintextOnce(t *testing.T) {
	u := authedUser(t)
	svc := &fakeAPITokenService{genPlaintext: "cmg_supersecretvalue", genTok: apitoken.Token{ID: uuid.New(), UserID: u.ID, Name: "ci-runner", LastFour: "alue"}}
	h := NewAccountTokensHandler(svc, testTokensShell(), func(*http.Request) string { return "csrf-tok" })
	fixed := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return fixed }

	req := httptest.NewRequest(http.MethodPost, "/account/tokens", strings.NewReader("name=ci-runner"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	if !svc.genCalled {
		t.Fatal("Generate should have been called")
	}
	if svc.genUserID != u.ID {
		t.Errorf("Generate called with userID = %v, want %v", svc.genUserID, u.ID)
	}
	if svc.genName != "ci-runner" {
		t.Errorf("Generate called with name = %q, want %q", svc.genName, "ci-runner")
	}
	if svc.genExpiresAt != nil {
		t.Errorf("Generate called with expiresAt = %v, want nil (no expiry supplied)", svc.genExpiresAt)
	}
	body := rec.Body.String()
	if strings.Count(body, "cmg_supersecretvalue") != 1 {
		t.Errorf("expected the plaintext to appear exactly once, body:\n%s", body)
	}

	// A subsequent List must NOT contain the plaintext anywhere.
	svc.tokens = []apitoken.Token{svc.genTok}
	req2 := httptest.NewRequest(http.MethodGet, "/account/tokens", nil)
	req2 = req2.WithContext(withUser(req2.Context(), u))
	rec2 := httptest.NewRecorder()
	h.List(rec2, req2)
	if strings.Contains(rec2.Body.String(), "cmg_supersecretvalue") {
		t.Error("plaintext must not reappear on a subsequent List render")
	}
}

func TestAccountTokensCreate_EmptyNameRejectedWithoutCallingGenerate(t *testing.T) {
	u := authedUser(t)
	svc := &fakeAPITokenService{}
	h := NewAccountTokensHandler(svc, testTokensShell(), func(*http.Request) string { return "csrf-tok" })

	req := httptest.NewRequest(http.MethodPost, "/account/tokens", strings.NewReader("name=   "))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422\n%s", rec.Code, rec.Body.String())
	}
	if svc.genCalled {
		t.Error("Generate must not be called for an empty name")
	}
	if !strings.Contains(rec.Body.String(), "Name is required.") {
		t.Error("expected a name-required field error")
	}
}

func TestAccountTokensCreate_InvalidExpiryDaysRejectedWithoutCallingGenerate(t *testing.T) {
	for _, raw := range []string{"abc", "-5", "0"} {
		u := authedUser(t)
		svc := &fakeAPITokenService{}
		h := NewAccountTokensHandler(svc, testTokensShell(), func(*http.Request) string { return "csrf-tok" })

		req := httptest.NewRequest(http.MethodPost, "/account/tokens", strings.NewReader("name=ci&expires_days="+raw))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(withUser(req.Context(), u))
		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expires_days=%q: code = %d, want 422\n%s", raw, rec.Code, rec.Body.String())
		}
		if svc.genCalled {
			t.Errorf("expires_days=%q: Generate must not be called for an invalid expiry", raw)
		}
	}
}

func TestAccountTokensCreate_ExpiryDaysComputedFromInjectedClock(t *testing.T) {
	u := authedUser(t)
	svc := &fakeAPITokenService{genPlaintext: "cmg_x", genTok: apitoken.Token{ID: uuid.New(), UserID: u.ID}}
	h := NewAccountTokensHandler(svc, testTokensShell(), func(*http.Request) string { return "csrf-tok" })
	fixed := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return fixed }

	req := httptest.NewRequest(http.MethodPost, "/account/tokens", strings.NewReader("name=ci&expires_days=30"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	want := fixed.Add(30 * 24 * time.Hour)
	if svc.genExpiresAt == nil || !svc.genExpiresAt.Equal(want) {
		t.Errorf("expiresAt = %v, want %v", svc.genExpiresAt, want)
	}
}

func TestAccountTokensRevoke_AlwaysPassesCurrentUserIDAndRedirects(t *testing.T) {
	u := authedUser(t)
	otherUsersTokenID := uuid.New()
	svc := &fakeAPITokenService{}
	h := NewAccountTokensHandler(svc, testTokensShell(), func(*http.Request) string { return "csrf-tok" })

	req := httptest.NewRequest(http.MethodPost, "/account/tokens/"+otherUsersTokenID.String()+"/revoke", nil)
	req = withChiParam(req, "id", otherUsersTokenID.String())
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.Revoke(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/account/tokens?revoked=1" {
		t.Errorf("redirect Location = %q", loc)
	}
	if !svc.revokeCalled {
		t.Fatal("Revoke should have been called")
	}
	if svc.revokeID != otherUsersTokenID {
		t.Errorf("Revoke id = %v, want %v", svc.revokeID, otherUsersTokenID)
	}
	// Owner-scoping: the handler must ALWAYS pass the authenticated user's id,
	// never one derived from the request, regardless of which id is targeted.
	if svc.revokeUserID != u.ID {
		t.Errorf("Revoke userID = %v, want %v (the authenticated user, not a request-derived id)", svc.revokeUserID, u.ID)
	}
}

func TestAccountTokensRevoke_BadUUIDRejected(t *testing.T) {
	u := authedUser(t)
	svc := &fakeAPITokenService{}
	h := NewAccountTokensHandler(svc, testTokensShell(), func(*http.Request) string { return "csrf-tok" })

	req := httptest.NewRequest(http.MethodPost, "/account/tokens/not-a-uuid/revoke", nil)
	req = withChiParam(req, "id", "not-a-uuid")
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.Revoke(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
	if svc.revokeCalled {
		t.Error("Revoke must not be called for a malformed id")
	}
}

func TestAccountTokensRevoke_ServiceErrorStillRedirectsGracefully(t *testing.T) {
	u := authedUser(t)
	svc := &fakeAPITokenService{revokeErr: apitoken.ErrNotFound}
	h := NewAccountTokensHandler(svc, testTokensShell(), func(*http.Request) string { return "csrf-tok" })

	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/account/tokens/"+id.String()+"/revoke", nil)
	req = withChiParam(req, "id", id.String())
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.Revoke(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303 (already-gone should still redirect gracefully)", rec.Code)
	}
}
