package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
)

// fakeVerifier is a table-driven APITokenVerifier: it maps a plaintext token to
// a (userID, ok) result, or returns errAny when errFor matches.
type fakeVerifier struct {
	valid  map[string]uuid.UUID
	errFor string
	err    error
}

func (f fakeVerifier) Verify(_ context.Context, plaintext string) (uuid.UUID, bool, error) {
	if f.errFor != "" && plaintext == f.errFor {
		return uuid.Nil, false, f.err
	}
	if id, ok := f.valid[plaintext]; ok {
		return id, true, nil
	}
	return uuid.Nil, false, nil
}

// sawUser records whether the downstream handler observed a context user.
func sawUser(seen *bool, gotID *uuid.UUID) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := UserFromContext(r.Context()); ok {
			*seen = true
			*gotID = u.ID
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestAPITokenAuthValidBearerSetsUser(t *testing.T) {
	id := uuid.New()
	users := fakeUsers{users: map[uuid.UUID]accounts.User{id: {ID: id, Email: "a@b.com"}}}
	m := NewAuthMiddleware(newFakeSession(), users, fakeAuthz{})
	verifier := fakeVerifier{valid: map[string]uuid.UUID{"cmg_good": id}}

	var seen bool
	var gotID uuid.UUID
	h := m.APITokenAuth(verifier)(sawUser(&seen, &gotID))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	req.Header.Set("Authorization", "Bearer cmg_good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !seen || gotID != id {
		t.Errorf("downstream did not see the authenticated user (seen=%v id=%v)", seen, gotID)
	}
}

func TestAPITokenAuthMissingHeaderStaysAnonymous(t *testing.T) {
	m := NewAuthMiddleware(newFakeSession(), fakeUsers{}, fakeAuthz{})
	verifier := fakeVerifier{}

	var seen bool
	var gotID uuid.UUID
	h := m.APITokenAuth(verifier)(sawUser(&seen, &gotID))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (falls through to next)", rec.Code)
	}
	if seen {
		t.Error("no user should be present without an Authorization header")
	}
}

func TestAPITokenAuthMissingHeaderThenPermissionGate401(t *testing.T) {
	// The realistic chain: token-auth (no header) then RequirePermission. The
	// absent user must yield a 401 from the gate, not a fall-through to the handler.
	m := NewAuthMiddleware(newFakeSession(), fakeUsers{}, fakeAuthz{})
	gate := m.RequirePermission(accounts.ActionRead, accounts.SubjectPost)
	h := m.APITokenAuth(fakeVerifier{})(gate(okHandler()))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAPITokenAuthBadTokenHard401(t *testing.T) {
	m := NewAuthMiddleware(newFakeSession(), fakeUsers{}, fakeAuthz{})
	verifier := fakeVerifier{valid: map[string]uuid.UUID{"cmg_good": uuid.New()}}

	var seen bool
	var gotID uuid.UUID
	h := m.APITokenAuth(verifier)(sawUser(&seen, &gotID))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	req.Header.Set("Authorization", "Bearer cmg_wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (bad token is a hard 401)", rec.Code)
	}
	if seen {
		t.Error("a bad token must not fall through to the handler")
	}
}

func TestAPITokenAuthVerifierErrorFailsClosed(t *testing.T) {
	m := NewAuthMiddleware(newFakeSession(), fakeUsers{}, fakeAuthz{})
	verifier := fakeVerifier{errFor: "cmg_boom", err: context.DeadlineExceeded}

	h := m.APITokenAuth(verifier)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	req.Header.Set("Authorization", "Bearer cmg_boom")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (fail closed on verifier error)", rec.Code)
	}
}

func TestAPITokenAuthValidTokenUnknownUser401(t *testing.T) {
	// A token verifies but its user no longer exists (deleted): fail closed.
	id := uuid.New()
	m := NewAuthMiddleware(newFakeSession(), fakeUsers{}, fakeAuthz{}) // empty user store
	verifier := fakeVerifier{valid: map[string]uuid.UUID{"cmg_good": id}}

	h := m.APITokenAuth(verifier)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	req.Header.Set("Authorization", "Bearer cmg_good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (user load failure fails closed)", rec.Code)
	}
}
