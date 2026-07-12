package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/httpx"
)

// sessionUserKey is the scs session key holding the authenticated user's id.
const sessionUserKey = "user_id"

// sessionPwdEpochKey holds the user's password_changed_at (as Unix nanoseconds)
// at the moment the session was minted. CurrentUser rejects any session whose
// stored epoch is older than the user's current PasswordChangedAt, so a password
// reset/change globally invalidates all previously issued sessions.
const sessionPwdEpochKey = "pwd_epoch"

// SessionManager is the subset of scs the auth middleware needs. The concrete
// *scs.SessionManager satisfies it.
type SessionManager interface {
	GetString(ctx context.Context, key string) string
	Put(ctx context.Context, key string, val interface{})
	Remove(ctx context.Context, key string)
	RenewToken(ctx context.Context) error
}

// UserLoader loads a user by id for the CurrentUser middleware.
type UserLoader interface {
	GetByID(ctx context.Context, id uuid.UUID) (accounts.User, error)
}

// PermissionChecker answers authorization questions. *accounts.Authorizer
// satisfies it.
type PermissionChecker interface {
	Can(ctx context.Context, userID uuid.UUID, action, subject string) bool
}

// AuthMiddleware bundles the session-backed authentication and authorization
// middleware. It is constructed with explicit dependencies.
type AuthMiddleware struct {
	sessions SessionManager
	users    UserLoader
	authz    PermissionChecker
}

// NewAuthMiddleware constructs an AuthMiddleware.
func NewAuthMiddleware(sessions SessionManager, users UserLoader, authz PermissionChecker) *AuthMiddleware {
	return &AuthMiddleware{sessions: sessions, users: users, authz: authz}
}

// Login stores the user id in the session, rotating the token to prevent
// session fixation. It also records the user's password_changed_at epoch so the
// session can be globally invalidated by a later credential change. Call from
// the login handler after the service authenticates.
func (m *AuthMiddleware) Login(ctx context.Context, userID uuid.UUID, passwordChangedAt time.Time) error {
	if err := m.sessions.RenewToken(ctx); err != nil {
		return err
	}
	m.sessions.Put(ctx, sessionUserKey, userID.String())
	m.sessions.Put(ctx, sessionPwdEpochKey, strconv.FormatInt(passwordChangedAt.UnixNano(), 10))
	return nil
}

// Logout clears the session user, rotating the token.
func (m *AuthMiddleware) Logout(ctx context.Context) error {
	m.sessions.Remove(ctx, sessionUserKey)
	m.sessions.Remove(ctx, sessionPwdEpochKey)
	return m.sessions.RenewToken(ctx)
}

// CurrentUser loads the authenticated user (if any) from the session into the
// request context. It never blocks the request: an absent/invalid session
// simply yields no user, leaving authorization to RequireAuth/RequirePermission.
func (m *AuthMiddleware) CurrentUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := m.sessions.GetString(r.Context(), sessionUserKey)
		if raw == "" {
			next.ServeHTTP(w, r)
			return
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		u, err := m.users.GetByID(r.Context(), id)
		if err != nil {
			// Stale session referencing a deleted user: drop it.
			m.sessions.Remove(r.Context(), sessionUserKey)
			m.sessions.Remove(r.Context(), sessionPwdEpochKey)
			next.ServeHTTP(w, r)
			return
		}
		// Reject sessions minted before the user's last credential change: a
		// password reset/change bumps PasswordChangedAt, globally logging out every
		// previously issued session. Missing/unparseable epoch is treated as stale.
		epoch, perr := strconv.ParseInt(m.sessions.GetString(r.Context(), sessionPwdEpochKey), 10, 64)
		if perr != nil || epoch < u.PasswordChangedAt.UnixNano() {
			m.sessions.Remove(r.Context(), sessionUserKey)
			m.sessions.Remove(r.Context(), sessionPwdEpochKey)
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

// RequireAuth gates a route on an authenticated user. Browser requests are
// redirected to /login; API requests (Accept: application/json or /api/ paths)
// receive 401.
func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFromContext(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}
		if wantsJSON(r) {
			httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

// RequireGuest gates a route on the absence of an authenticated user (e.g. the
// login/signup pages). Authenticated users are redirected to /admin.
func (m *AuthMiddleware) RequireGuest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFromContext(r.Context()); ok {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequirePermission gates a route on the current user holding (action, subject).
// Unauthenticated users are handled as by RequireAuth; authenticated-but-denied
// users get 403.
func (m *AuthMiddleware) RequirePermission(action, subject string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok {
				if wantsJSON(r) {
					httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			if !m.authz.Can(r.Context(), u.ID, action, subject) {
				if wantsJSON(r) {
					httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
					return
				}
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// wantsJSON reports whether the request prefers a JSON (API) response.
func wantsJSON(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}
