package accounts

import (
	"context"
	"errors"
	"net/http"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// OAuthService is the subset of *AuthService the OAuth handler calls. Declaring
// it keeps the handler a pure HTTP boundary and testable with a fake.
type OAuthService interface {
	LoginWithOAuth(ctx context.Context, id OAuthIdentity) (User, error)
}

// gothCompleter abstracts goth's CompleteUserAuth so the handler is testable
// without a live provider round trip. The default value calls gothic.
type gothCompleter func(w http.ResponseWriter, r *http.Request) (goth.User, error)

// gothBeginner abstracts goth's BeginAuthHandler.
type gothBeginner func(w http.ResponseWriter, r *http.Request)

// ProviderResolver extracts the provider name from the request (e.g. the chi URL
// param). gothic keys off a request value, so we set it before delegating.
type ProviderResolver func(r *http.Request) string

// OAuthHandler is the thin HTTP boundary for social login. It resolves the
// provider, drives goth's begin/callback, maps goth's user to an OAuthIdentity,
// and hands business logic to the service. ZERO business logic lives here.
type OAuthHandler struct {
	svc      OAuthService
	session  SessionLogin
	resolve  ProviderResolver
	begin    gothBeginner
	complete gothCompleter
}

// NewOAuthHandler constructs an OAuthHandler. resolve extracts the provider name
// from the request (router-specific); the goth begin/complete funcs default to
// gothic when nil so tests can substitute fakes.
func NewOAuthHandler(svc OAuthService, session SessionLogin, resolve ProviderResolver) *OAuthHandler {
	return &OAuthHandler{
		svc:      svc,
		session:  session,
		resolve:  resolve,
		begin:    gothic.BeginAuthHandler,
		complete: gothic.CompleteUserAuth,
	}
}

// withProvider stores the provider name where gothic expects it. gothic reads
// the provider from the request context key gothic.ProviderParamKey (falling
// back to the "provider" query param), so we inject it from the chi URL param.
func withProvider(r *http.Request, provider string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), gothic.ProviderParamKey, provider))
}

// Begin starts the OAuth flow: it redirects the browser to the provider's
// consent screen. CSRF/state is handled by goth via the gothic store.
func (h *OAuthHandler) Begin(w http.ResponseWriter, r *http.Request) {
	provider := h.resolve(r)
	r = withProvider(r, provider)
	// If the user is already mid-flow with a valid session, CompleteUserAuth
	// would succeed; otherwise BeginAuthHandler issues the redirect.
	h.begin(w, r)
}

// Callback completes the OAuth flow: it validates the provider response, maps it
// to an OAuthIdentity, runs the service login/link/create flow, establishes the
// application session (reusing the password-login session path), and redirects
// to /admin.
func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	provider := h.resolve(r)
	r = withProvider(r, provider)

	gu, err := h.complete(w, r)
	if err != nil {
		h.renderError(w, r, "We could not complete sign-in with that provider. Please try again.")
		return
	}

	id := OAuthIdentity{
		Provider:       provider,
		ProviderUserID: gu.UserID,
		Email:          gu.Email,
		Name:           displayName(gu),
		AvatarURL:      gu.AvatarURL,
	}

	user, err := h.svc.LoginWithOAuth(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, ErrOAuthSignupDisabled):
			h.renderError(w, r, "Registration is disabled. Ask an administrator to create your account first.")
		case errors.Is(err, ErrOAuthNoEmail):
			h.renderError(w, r, "Your provider did not share an email address, so we cannot sign you in.")
		default:
			h.renderError(w, r, "Something went wrong signing you in. Please try again.")
		}
		return
	}

	// Reuse the SAME session-login path as password login: userID + password
	// epoch. No bespoke session logic here.
	if err := h.session.Login(r.Context(), user.ID, user.PasswordChangedAt); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// displayName prefers the provider's Name, then NickName, then the email local
// part is left to the service; here we just pick the best available label.
func displayName(gu goth.User) string {
	if gu.Name != "" {
		return gu.Name
	}
	if gu.NickName != "" {
		return gu.NickName
	}
	return gu.FirstName
}

func (h *OAuthHandler) renderError(w http.ResponseWriter, r *http.Request, msg string) {
	f := webtempl.AuthForm{Values: map[string]string{}, FieldErrors: map[string]string{}, Error: msg}
	if err := render.Component(r.Context(), w, http.StatusUnauthorized, webtempl.LoginPage(f)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
