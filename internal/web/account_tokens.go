package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/apitoken"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// maxTokenNameLen caps the operator-supplied token label so a runaway input
// can't bloat the row/list rendering.
const maxTokenNameLen = 100

// APITokenService is the subset of *apitoken.Service the self-service token
// area needs: list/generate/revoke, all scoped to a single owner. Declaring it
// here keeps the handler a thin, fakeable HTTP boundary.
type APITokenService interface {
	// List returns every token owned by userID, newest first.
	List(ctx context.Context, userID uuid.UUID) ([]apitoken.Token, error)
	// Generate mints a new token for userID and returns its PLAINTEXT once.
	Generate(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (string, apitoken.Token, error)
	// Revoke marks the token revoked, scoped to (id, userID) so a caller can
	// only ever revoke its own tokens.
	Revoke(ctx context.Context, id, userID uuid.UUID) error
}

// AccountTokensHandler is the thin HTTP boundary for the self-service
// /account/tokens area: list, create (reveal-once), and revoke the CURRENT
// user's own API tokens. Every call is scoped to UserFromContext's id, never
// an id from the request, so a user can only ever see/act on their own
// tokens (no IDOR).
type AccountTokensHandler struct {
	svc   APITokenService
	shell adminShellDeps
	csrf  func(*http.Request) string
	now   func() time.Time
}

// NewAccountTokensHandler constructs the handler with explicit dependencies.
// now defaults to time.Now; tests inject a fixed clock for deterministic
// expiry/status computation.
func NewAccountTokensHandler(svc APITokenService, shell adminShellDeps, csrf func(*http.Request) string) *AccountTokensHandler {
	return &AccountTokensHandler{svc: svc, shell: shell, csrf: csrf, now: time.Now}
}

// List renders the current user's tokens plus the create form.
func (h *AccountTokensHandler) List(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	rows, err := h.buildRows(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	view := webtempl.TokensView{
		Shell:     h.shell.buildShell(r, "API tokens"),
		CSRFToken: h.csrf(r),
		Rows:      rows,
		Revoked:   r.URL.Query().Get("revoked") == "1",
	}
	h.render(w, r, http.StatusOK, view)
}

// Create validates the submitted name (required, trimmed, capped) and
// optional expiry-in-days, then mints a token via Generate. On success it
// re-renders the list with a ONE-TIME reveal banner containing the plaintext;
// on a validation error it re-renders with a field error and never calls
// Generate.
func (h *AccountTokensHandler) Create(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	_ = r.ParseForm()
	name := strings.TrimSpace(r.PostFormValue("name"))

	switch {
	case name == "":
		h.renderCreateError(w, r, u.ID, "Name is required.")
		return
	case len(name) > maxTokenNameLen:
		h.renderCreateError(w, r, u.ID, "Name must be 100 characters or fewer.")
		return
	}

	var expiresAt *time.Time
	if raw := strings.TrimSpace(r.PostFormValue("expires_days")); raw != "" {
		days, convErr := strconv.Atoi(raw)
		if convErr != nil || days <= 0 {
			// Reject a malformed/negative value rather than silently minting a
			// never-expiring token the user thought would expire.
			h.renderCreateError(w, r, u.ID, "Expiry must be a positive number of days, or left blank for no expiry.")
			return
		}
		t := h.now().Add(time.Duration(days) * 24 * time.Hour)
		expiresAt = &t
	}

	plaintext, _, err := h.svc.Generate(r.Context(), u.ID, name, expiresAt)
	if err != nil {
		h.renderCreateError(w, r, u.ID, "Something went wrong. Please try again.")
		return
	}

	rows, err := h.buildRows(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	view := webtempl.TokensView{
		Shell:        h.shell.buildShell(r, "API tokens"),
		CSRFToken:    h.csrf(r),
		Rows:         rows,
		Revealed:     plaintext,
		RevealedName: name,
	}
	h.render(w, r, http.StatusOK, view)
}

// Revoke revokes the token identified by the {id} route param, scoped to the
// CURRENT user (never a user id from the request), then redirects back to the
// list. A malformed id is rejected with 400 before the service is called; any
// service error (already revoked, foreign, or a transient failure) is treated
// as "already gone" and still redirects gracefully, since Revoke is
// owner-scoped and idempotent, and surfacing the difference would leak
// whether a foreign id exists.
func (h *AccountTokensHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid token id", http.StatusBadRequest)
		return
	}
	_ = h.svc.Revoke(r.Context(), id, u.ID)
	http.Redirect(w, r, "/account/tokens?revoked=1", http.StatusSeeOther)
}

// renderCreateError re-lists the current tokens and re-renders the page with
// a create-form field error, at 422, without ever calling Generate.
func (h *AccountTokensHandler) renderCreateError(w http.ResponseWriter, r *http.Request, userID uuid.UUID, msg string) {
	rows, err := h.buildRows(r.Context(), userID)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	view := webtempl.TokensView{
		Shell:     h.shell.buildShell(r, "API tokens"),
		CSRFToken: h.csrf(r),
		Rows:      rows,
		NameError: msg,
	}
	h.render(w, r, http.StatusUnprocessableEntity, view)
}

// buildRows lists userID's tokens and maps them to display-safe rows (masked
// last-four, pre-formatted dates, computed status). It never carries the hash
// or plaintext.
func (h *AccountTokensHandler) buildRows(ctx context.Context, userID uuid.UUID) ([]webtempl.TokenRow, error) {
	toks, err := h.svc.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	now := h.now()
	rows := make([]webtempl.TokenRow, 0, len(toks))
	for _, t := range toks {
		rows = append(rows, webtempl.TokenRow{
			ID:         t.ID.String(),
			Name:       t.Name,
			LastFour:   t.LastFour,
			CreatedAt:  t.CreatedAt.Format("Jan 2, 2006 15:04"),
			LastUsedAt: formatOptionalTime(t.LastUsedAt),
			ExpiresAt:  formatOptionalTime(t.ExpiresAt),
			Status:     tokenStatus(t, now),
			RevokeURL:  "/account/tokens/" + t.ID.String() + "/revoke",
		})
	}
	return rows, nil
}

// tokenStatus computes the display status against now: revoked takes
// precedence over expiry, otherwise a past ExpiresAt marks it expired.
func tokenStatus(t apitoken.Token, now time.Time) string {
	switch {
	case t.RevokedAt != nil:
		return "revoked"
	case t.ExpiresAt != nil && t.ExpiresAt.Before(now):
		return "expired"
	default:
		return "active"
	}
}

// formatOptionalTime formats a nilable timestamp, or "Never" when nil.
func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return "Never"
	}
	return t.Format("Jan 2, 2006 15:04")
}

func (h *AccountTokensHandler) render(w http.ResponseWriter, r *http.Request, status int, v webtempl.TokensView) {
	if err := render.Component(r.Context(), w, status, webtempl.AccountTokensPage(v)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
