package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/httpx"
)

// APITokenVerifier resolves a presented plaintext bearer token to its owner's
// user id. *apitoken.Service satisfies it. A (uuid.Nil, false, nil) result means
// "no such valid token"; a non-nil error means the verifier itself failed.
type APITokenVerifier interface {
	Verify(ctx context.Context, plaintext string) (uuid.UUID, bool, error)
}

// APITokenAuth builds a stateless bearer-token authentication middleware for the
// REST API. It resolves an Authorization: Bearer <token> header to a user and
// stores that user in the request context with the SAME withUser used by the
// session middleware, so the existing RequirePermission gate is the single RBAC
// source of truth on API routes.
//
// Behavior:
//   - No Authorization header, or a non-Bearer scheme: the request continues with
//     NO user in context. A downstream RequirePermission then returns 401.
//   - A Bearer token that verifies: the user is loaded and injected; next runs.
//   - A Bearer token that is invalid, or any verifier/user-load error: the
//     request is rejected immediately with 401 (fail closed). An explicitly
//     supplied bad credential is a hard 401, not a fall-through to anonymous.
func (m *AuthMiddleware) APITokenAuth(verifier APITokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				// No credential supplied: stay anonymous; the permission gate decides.
				next.ServeHTTP(w, r)
				return
			}
			userID, valid, err := verifier.Verify(r.Context(), token)
			if err != nil || !valid {
				unauthorizedJSON(w)
				return
			}
			u, err := m.users.GetByID(r.Context(), userID)
			if err != nil {
				unauthorizedJSON(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
		})
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
// The second result is false when the header is absent or not a Bearer scheme.
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

// unauthorizedJSON writes the API's 401 response.
func unauthorizedJSON(w http.ResponseWriter) {
	httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}
