package accounts

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// Service is the subset of *AuthService the handler calls. Declaring it keeps
// the handler a pure HTTP boundary and testable with a fake.
type Service interface {
	Register(ctx context.Context, in RegisterInput) (User, error)
	Login(ctx context.Context, in LoginInput) (User, error)
	RequestPasswordReset(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, token, newPassword string) error
	VerifyEmail(ctx context.Context, token string) error
}

// SessionLogin is the subset of the web auth middleware the handler needs to
// start/stop a session. The web.AuthMiddleware satisfies it.
type SessionLogin interface {
	Login(ctx context.Context, userID uuid.UUID, passwordChangedAt time.Time) error
	Logout(ctx context.Context) error
}

// CSRFTokenFunc returns the CSRF token for a request (security.Token).
type CSRFTokenFunc func(r *http.Request) string

// Validator validates a DTO, returning a map of field->message on failure.
type Validator func(v any) map[string]string

// Handler is the thin HTTP boundary for auth pages. It decodes the form,
// validates the DTO, calls the service, and renders/redirects. ZERO business
// logic lives here.
type Handler struct {
	svc       Service
	session   SessionLogin
	csrf      CSRFTokenFunc
	validate  Validator
	providers []webtempl.OAuthProviderButton
}

// NewHandler constructs the auth Handler. providers are the enabled social-login
// providers shown on the login/signup pages; pass nil/empty when none are
// configured (the buttons then render as a no-op).
func NewHandler(svc Service, session SessionLogin, csrf CSRFTokenFunc, validate Validator, providers ...webtempl.OAuthProviderButton) *Handler {
	return &Handler{svc: svc, session: session, csrf: csrf, validate: validate, providers: providers}
}

func (h *Handler) form(r *http.Request) webtempl.AuthForm {
	return webtempl.AuthForm{
		CSRFToken:      h.csrf(r),
		Values:         map[string]string{},
		FieldErrors:    map[string]string{},
		OAuthProviders: h.providers,
	}
}

func (h *Handler) renderPage(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	if err := render.Component(r.Context(), w, status, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// --- Login -------------------------------------------------------------------

// ShowLogin renders the login page.
func (h *Handler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, r, http.StatusOK, webtempl.LoginPage(h.form(r)))
}

// SubmitLogin authenticates and starts a session, redirecting to /admin.
func (h *Handler) SubmitLogin(w http.ResponseWriter, r *http.Request) {
	f := h.form(r)
	identifier := r.PostFormValue("identifier")
	password := r.PostFormValue("password")
	f.Values["identifier"] = identifier

	user, err := h.svc.Login(r.Context(), LoginInput{Identifier: identifier, Password: password})
	if err != nil {
		switch {
		case errors.Is(err, ErrEmailNotVerified):
			f.Error = "Please verify your email address before signing in."
		default:
			f.Error = "Invalid email/username or password."
		}
		h.renderPage(w, r, http.StatusUnauthorized, webtempl.LoginPage(f))
		return
	}

	if err := h.session.Login(r.Context(), user.ID, user.PasswordChangedAt); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// --- Signup ------------------------------------------------------------------

// ShowSignup renders the registration page.
func (h *Handler) ShowSignup(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, r, http.StatusOK, webtempl.SignupPage(h.form(r)))
}

// SubmitSignup validates and registers, then shows a verification notice.
func (h *Handler) SubmitSignup(w http.ResponseWriter, r *http.Request) {
	f := h.form(r)
	dto := registerDTO{
		Name:     r.PostFormValue("name"),
		Username: r.PostFormValue("username"),
		Email:    r.PostFormValue("email"),
		Password: r.PostFormValue("password"),
	}
	f.Values["name"] = dto.Name
	f.Values["username"] = dto.Username
	f.Values["email"] = dto.Email

	if fieldErrs := h.validate(dto); len(fieldErrs) > 0 {
		f.FieldErrors = fieldErrs
		h.renderPage(w, r, http.StatusUnprocessableEntity, webtempl.SignupPage(f))
		return
	}

	_, err := h.svc.Register(r.Context(), RegisterInput{Name: dto.Name, Username: dto.Username, Email: dto.Email, Password: dto.Password})
	switch {
	case errors.Is(err, ErrSignupDisabled):
		f.Error = "Registration is currently disabled."
		h.renderPage(w, r, http.StatusForbidden, webtempl.SignupPage(f))
		return
	case errors.Is(err, ErrEmailTaken):
		// Do not over-disclose; field-level hint is acceptable on signup.
		f.FieldErrors["email"] = "An account with this email already exists."
		h.renderPage(w, r, http.StatusConflict, webtempl.SignupPage(f))
		return
	case errors.Is(err, ErrUsernameTaken):
		f.FieldErrors["username"] = "That username is already taken."
		h.renderPage(w, r, http.StatusConflict, webtempl.SignupPage(f))
		return
	case errors.Is(err, ErrInvalidUsername):
		f.FieldErrors["username"] = "Username must be 3–30 characters: letters, numbers, _ or -."
		h.renderPage(w, r, http.StatusUnprocessableEntity, webtempl.SignupPage(f))
		return
	case err != nil:
		f.Error = "Something went wrong. Please try again."
		h.renderPage(w, r, http.StatusInternalServerError, webtempl.SignupPage(f))
		return
	}

	notice := h.form(r)
	notice.Notice = "Account created. Check your email for a verification link."
	h.renderPage(w, r, http.StatusOK, webtempl.LoginPage(notice))
}

// --- Logout ------------------------------------------------------------------

// SubmitLogout ends the session and redirects to /login.
func (h *Handler) SubmitLogout(w http.ResponseWriter, r *http.Request) {
	if err := h.session.Logout(r.Context()); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Forgot / Reset ----------------------------------------------------------

// ShowForgotPassword renders the reset-request page.
func (h *Handler) ShowForgotPassword(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, r, http.StatusOK, webtempl.ForgotPasswordPage(h.form(r)))
}

// SubmitForgotPassword requests a reset. It ALWAYS shows the same success
// notice regardless of whether the email exists (anti-enumeration).
func (h *Handler) SubmitForgotPassword(w http.ResponseWriter, r *http.Request) {
	email := r.PostFormValue("email")
	if err := h.svc.RequestPasswordReset(r.Context(), email); err != nil {
		// Even on internal error we do not leak; log path handled upstream.
		http.Error(w, "request error", http.StatusInternalServerError)
		return
	}
	f := h.form(r)
	f.Notice = "If an account exists for that email, a reset link has been sent."
	h.renderPage(w, r, http.StatusOK, webtempl.ForgotPasswordPage(f))
}

// ShowResetPassword renders the new-password form for a token from the query.
func (h *Handler) ShowResetPassword(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	h.renderPage(w, r, http.StatusOK, webtempl.ResetPasswordPage(h.form(r), token))
}

// SubmitResetPassword validates and applies the new password.
func (h *Handler) SubmitResetPassword(w http.ResponseWriter, r *http.Request) {
	token := r.PostFormValue("token")
	f := h.form(r)
	dto := resetDTO{Password: r.PostFormValue("password")}

	if fieldErrs := h.validate(dto); len(fieldErrs) > 0 {
		f.FieldErrors = fieldErrs
		h.renderPage(w, r, http.StatusUnprocessableEntity, webtempl.ResetPasswordPage(f, token))
		return
	}

	err := h.svc.ResetPassword(r.Context(), token, dto.Password)
	switch {
	case errors.Is(err, ErrInvalidToken):
		f.Error = "This reset link is invalid or has expired. Request a new one."
		h.renderPage(w, r, http.StatusBadRequest, webtempl.ResetPasswordPage(f, token))
		return
	case err != nil:
		f.Error = "Something went wrong. Please try again."
		h.renderPage(w, r, http.StatusInternalServerError, webtempl.ResetPasswordPage(f, token))
		return
	}

	h.renderPage(w, r, http.StatusOK, webtempl.MessagePage(
		"Password updated", "Password updated", "Your password has been changed. You can now sign in.",
	))
}

// --- Verify email ------------------------------------------------------------

// VerifyEmail consumes a verification token from the query and shows the result.
func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	err := h.svc.VerifyEmail(r.Context(), token)
	if err != nil {
		h.renderPage(w, r, http.StatusBadRequest, webtempl.MessagePage(
			"Verification failed", "Verification failed",
			"This verification link is invalid or has expired.",
		))
		return
	}
	h.renderPage(w, r, http.StatusOK, webtempl.MessagePage(
		"Email verified", "Email verified", "Thanks — your email is verified. You can now sign in.",
	))
}
