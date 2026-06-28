package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	"github.com/huseyn0w/cmstack-go/internal/platform/storage"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// maxAvatarFormBytes bounds the multipart parse for avatar uploads at the avatar
// cap + slack for headers, so a huge body is rejected before buffering.
const maxAvatarFormBytes = storage.MaxAvatarBytes + (1 << 16)

// ProfileService is the subset of *accounts.ProfileService the account handler
// needs. Declaring it keeps the handler a thin HTTP boundary, testable with a
// fake.
type ProfileService interface {
	UpdateProfile(ctx context.Context, userID uuid.UUID, in accounts.UpdateProfileInput) (accounts.User, error)
	UpdateAvatar(ctx context.Context, userID uuid.UUID, up accounts.AvatarUpload) (accounts.User, error)
	AvatarURL(u accounts.User) string
}

// PasswordChanger is the subset of *accounts.AuthService used by the password
// change form (the existing ChangePassword method).
type PasswordChanger interface {
	ChangePassword(ctx context.Context, userID uuid.UUID, current, next string) error
}

// AccountHandler is the thin HTTP boundary for the self-service /account area.
// It decodes the form, delegates to the service, and renders/redirects — no
// business logic lives here.
type AccountHandler struct {
	profiles  ProfileService
	passwords PasswordChanger
	roles     RoleResolver
	authz     PermissionChecker
	csrf      func(*http.Request) string
	siteURL   string
}

// NewAccountHandler constructs the account handler with explicit dependencies.
func NewAccountHandler(profiles ProfileService, passwords PasswordChanger, roles RoleResolver, authz PermissionChecker, csrf func(*http.Request) string, siteURL string) *AccountHandler {
	return &AccountHandler{profiles: profiles, passwords: passwords, roles: roles, authz: authz, csrf: csrf, siteURL: siteURL}
}

// knownSocials is the ordered set of social networks the form renders. It mirrors
// the accounts service allow-list; the service is still the authority on storage.
var knownSocials = []string{"twitter", "github", "linkedin", "mastodon"}

// buildForm assembles the AccountForm view-model for the current user, reusing
// the shared admin-shell builder so the page lives inside the admin chrome.
func (h *AccountHandler) buildForm(r *http.Request) (accounts.User, webtempl.AccountForm) {
	u, _ := UserFromContext(r.Context())

	shell := adminShellDeps{authz: h.authz, roles: h.roles, csrf: h.csrf, siteURL: h.siteURL}.buildShell(r, "Account")

	csrf := ""
	if h.csrf != nil {
		csrf = h.csrf(r)
	}

	return u, webtempl.AccountForm{
		Shell:       shell,
		CSRFToken:   csrf,
		Name:        u.Name,
		Bio:         u.Bio,
		Website:     u.Website,
		AvatarURL:   h.profiles.AvatarURL(u),
		Socials:     u.SocialLinks,
		SocialOrder: knownSocials,
		FieldErrors: map[string]string{},
	}
}

func (h *AccountHandler) render(w http.ResponseWriter, r *http.Request, status int, f webtempl.AccountForm) {
	if err := render.Component(r.Context(), w, status, webtempl.AccountPage(f)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// Show renders the account editor.
func (h *AccountHandler) Show(w http.ResponseWriter, r *http.Request) {
	_, f := h.buildForm(r)
	h.render(w, r, http.StatusOK, f)
}

// SubmitProfile saves the name/bio/website/socials form.
func (h *AccountHandler) SubmitProfile(w http.ResponseWriter, r *http.Request) {
	u, f := h.buildForm(r)

	socials := map[string]string{}
	for _, key := range knownSocials {
		socials[key] = r.PostFormValue("social_" + key)
	}
	in := accounts.UpdateProfileInput{
		Name:        r.PostFormValue("name"),
		Bio:         r.PostFormValue("bio"),
		Website:     r.PostFormValue("website"),
		SocialLinks: socials,
	}
	// Reflect submitted values back so a re-render keeps the user's input.
	f.Name, f.Bio, f.Website, f.Socials = in.Name, in.Bio, in.Website, socials

	updated, err := h.profiles.UpdateProfile(r.Context(), u.ID, in)
	var verr accounts.ProfileValidationError
	switch {
	case errors.As(err, &verr):
		f.FieldErrors = verr.Fields
		h.render(w, r, http.StatusUnprocessableEntity, f)
		return
	case err != nil:
		f.FieldErrors["form"] = "Something went wrong. Please try again."
		h.render(w, r, http.StatusInternalServerError, f)
		return
	}

	f.Name, f.Bio, f.Website, f.Socials = updated.Name, updated.Bio, updated.Website, updated.SocialLinks
	f.AvatarURL = h.profiles.AvatarURL(updated)
	f.Saved = true
	h.render(w, r, http.StatusOK, f)
}

// SubmitAvatar validates (by magic bytes) and stores the uploaded avatar.
func (h *AccountHandler) SubmitAvatar(w http.ResponseWriter, r *http.Request) {
	u, f := h.buildForm(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarFormBytes)
	if err := r.ParseMultipartForm(maxAvatarFormBytes); err != nil {
		f.FieldErrors["avatar"] = "Upload was too large or malformed."
		h.render(w, r, http.StatusRequestEntityTooLarge, f)
		return
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		f.FieldErrors["avatar"] = "Choose an image to upload."
		h.render(w, r, http.StatusUnprocessableEntity, f)
		return
	}
	defer func() { _ = file.Close() }()

	// Magic-byte validation: filename and declared type are ignored.
	validated, err := storage.ValidateAvatar(file)
	if err != nil {
		f.FieldErrors["avatar"] = avatarErrorMessage(err)
		h.render(w, r, http.StatusUnprocessableEntity, f)
		return
	}

	updated, err := h.profiles.UpdateAvatar(r.Context(), u.ID, validated)
	if err != nil {
		f.FieldErrors["avatar"] = "Could not save the avatar. Please try again."
		h.render(w, r, http.StatusInternalServerError, f)
		return
	}
	f.AvatarURL = h.profiles.AvatarURL(updated)
	f.Saved = true
	h.render(w, r, http.StatusOK, f)
}

// SubmitPassword changes the password via the existing ChangePassword service.
func (h *AccountHandler) SubmitPassword(w http.ResponseWriter, r *http.Request) {
	u, f := h.buildForm(r)

	current := r.PostFormValue("current_password")
	next := r.PostFormValue("new_password")
	confirm := r.PostFormValue("confirm_password")

	switch {
	case len(next) < 8:
		f.PwdError = "New password must be at least 8 characters."
		h.render(w, r, http.StatusUnprocessableEntity, f)
		return
	case next != confirm:
		f.PwdError = "New password and confirmation do not match."
		h.render(w, r, http.StatusUnprocessableEntity, f)
		return
	}

	err := h.passwords.ChangePassword(r.Context(), u.ID, current, next)
	switch {
	case errors.Is(err, accounts.ErrInvalidCredentials):
		f.PwdError = "Your current password is incorrect."
		h.render(w, r, http.StatusUnprocessableEntity, f)
		return
	case err != nil:
		f.PwdError = "Something went wrong. Please try again."
		h.render(w, r, http.StatusInternalServerError, f)
		return
	}

	f.PwdSaved = true
	h.render(w, r, http.StatusOK, f)
}

// avatarErrorMessage maps a storage validation error to a user-facing message.
func avatarErrorMessage(err error) string {
	switch {
	case errors.Is(err, storage.ErrAvatarTooLarge):
		return "Image is too large (2 MB max)."
	case errors.Is(err, storage.ErrAvatarType):
		return "Please upload a PNG, JPEG, WebP or GIF image."
	case errors.Is(err, storage.ErrAvatarEmpty):
		return "The uploaded file was empty."
	default:
		return "That file could not be processed as an image."
	}
}
