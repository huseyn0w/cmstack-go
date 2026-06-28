package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
)

// fakeSession is an in-memory SessionManager keyed off a single map (tests use
// one request at a time).
type fakeSession struct {
	values map[string]string
	renews int
}

func newFakeSession() *fakeSession { return &fakeSession{values: map[string]string{}} }

func (s *fakeSession) GetString(_ context.Context, key string) string { return s.values[key] }
func (s *fakeSession) Put(_ context.Context, key string, val interface{}) {
	s.values[key] = val.(string)
}
func (s *fakeSession) Remove(_ context.Context, key string) { delete(s.values, key) }
func (s *fakeSession) RenewToken(_ context.Context) error   { s.renews++; return nil }

type fakeUsers struct {
	users map[uuid.UUID]accounts.User
}

func (f fakeUsers) GetByID(_ context.Context, id uuid.UUID) (accounts.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return accounts.User{}, accounts.ErrNotFound
}

type fakeAuthz struct {
	allow map[string]bool // "action:subject" -> allowed
}

func (f fakeAuthz) Can(_ context.Context, _ uuid.UUID, action, subject string) bool {
	return f.allow[action+":"+subject]
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestLoginStoresUserAndRenewsToken(t *testing.T) {
	sess := newFakeSession()
	m := NewAuthMiddleware(sess, fakeUsers{}, fakeAuthz{})
	id := uuid.New()
	if err := m.Login(context.Background(), id, time.Now()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if sess.values[sessionUserKey] != id.String() {
		t.Error("user id not stored in session")
	}
	if sess.renews == 0 {
		t.Error("session token not renewed (fixation protection)")
	}
}

// TestCurrentUserRejectsSessionMintedBeforePasswordChange guards Fix 3: a
// session minted under an older password_changed_at must be rejected (and
// cleared) once the user's PasswordChangedAt advances, forcing a global logout
// after a credential change.
func TestCurrentUserRejectsSessionMintedBeforePasswordChange(t *testing.T) {
	id := uuid.New()
	changedAt := time.Now()

	sess := newFakeSession()
	m := NewAuthMiddleware(sess, fakeUsers{users: map[uuid.UUID]accounts.User{
		id: {ID: id, Email: "x@y.com", PasswordChangedAt: changedAt},
	}}, fakeAuthz{})

	// Mint a session at the current epoch — it must be accepted.
	if err := m.Login(context.Background(), id, changedAt); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got := userPresence(t, m, sess); !got {
		t.Fatal("freshly minted session should be accepted")
	}

	// The user changes their password: PasswordChangedAt advances.
	m.users = fakeUsers{users: map[uuid.UUID]accounts.User{
		id: {ID: id, Email: "x@y.com", PasswordChangedAt: changedAt.Add(time.Second)},
	}}
	if got := userPresence(t, m, sess); got {
		t.Error("session minted before the password change must be rejected")
	}
	if sess.values[sessionUserKey] != "" {
		t.Error("rejected stale session must be cleared")
	}
}

// TestCurrentUserAcceptsSessionMintedAfterPasswordChange guards Fix 3: a fresh
// login after a password change works.
func TestCurrentUserAcceptsSessionMintedAfterPasswordChange(t *testing.T) {
	id := uuid.New()
	newEpoch := time.Now()

	sess := newFakeSession()
	m := NewAuthMiddleware(sess, fakeUsers{users: map[uuid.UUID]accounts.User{
		id: {ID: id, Email: "x@y.com", PasswordChangedAt: newEpoch},
	}}, fakeAuthz{})

	if err := m.Login(context.Background(), id, newEpoch); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got := userPresence(t, m, sess); !got {
		t.Error("session minted at the current epoch must be accepted")
	}
}

// userPresence runs CurrentUser against the session and reports whether a user
// was loaded into the request context.
func userPresence(t *testing.T, m *AuthMiddleware, _ *fakeSession) bool {
	t.Helper()
	var present bool
	h := m.CurrentUser(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, present = UserFromContext(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	return present
}

func TestCurrentUserLoadsIntoContext(t *testing.T) {
	id := uuid.New()
	changedAt := time.Now()
	sess := newFakeSession()
	sess.values[sessionUserKey] = id.String()
	sess.values[sessionPwdEpochKey] = strconv.FormatInt(changedAt.UnixNano(), 10)
	users := fakeUsers{users: map[uuid.UUID]accounts.User{id: {ID: id, Email: "x@y.com", PasswordChangedAt: changedAt}}}
	m := NewAuthMiddleware(sess, users, fakeAuthz{})

	var gotUser accounts.User
	var present bool
	h := m.CurrentUser(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotUser, present = UserFromContext(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !present || gotUser.ID != id {
		t.Errorf("expected user %s in context, present=%v got=%v", id, present, gotUser.ID)
	}
}

func TestRequireAuthRedirectsBrowser(t *testing.T) {
	m := NewAuthMiddleware(newFakeSession(), fakeUsers{}, fakeAuthz{})
	rec := httptest.NewRecorder()
	m.RequireAuth(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303 redirect", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("redirect to %q, want /login", loc)
	}
}

func TestRequireAuth401ForAPI(t *testing.T) {
	m := NewAuthMiddleware(newFakeSession(), fakeUsers{}, fakeAuthz{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	m.RequireAuth(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
}

func TestRequireGuestRedirectsAuthenticated(t *testing.T) {
	id := uuid.New()
	m := NewAuthMiddleware(newFakeSession(), fakeUsers{}, fakeAuthz{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil).
		WithContext(withUser(context.Background(), accounts.User{ID: id}))
	m.RequireGuest(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303", rec.Code)
	}
}

func TestRequirePermission403(t *testing.T) {
	id := uuid.New()
	m := NewAuthMiddleware(newFakeSession(), fakeUsers{}, fakeAuthz{allow: map[string]bool{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil).
		WithContext(withUser(context.Background(), accounts.User{ID: id}))
	m.RequirePermission(accounts.ActionRead, accounts.SubjectUser)(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}
}

func TestRequirePermissionAllows(t *testing.T) {
	id := uuid.New()
	m := NewAuthMiddleware(newFakeSession(), fakeUsers{}, fakeAuthz{allow: map[string]bool{"read:user": true}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil).
		WithContext(withUser(context.Background(), accounts.User{ID: id}))
	m.RequirePermission(accounts.ActionRead, accounts.SubjectUser)(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
}
