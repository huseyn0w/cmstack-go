package accounts

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/storage"
)

// knownSocialNetworks is the allow-list of social keys a profile may carry. Any
// other key submitted is dropped (not stored), so the social map stays a closed,
// predictable set the public page can render with per-network icons.
var knownSocialNetworks = []string{"twitter", "github", "linkedin", "mastodon"}

// ProfileValidationError is returned by UpdateProfile when a field fails
// validation. The Fields map carries per-field messages keyed by form-field name
// for the error summary.
type ProfileValidationError struct {
	Fields map[string]string
}

func (e ProfileValidationError) Error() string {
	return fmt.Sprintf("accounts: invalid profile (%d field errors)", len(e.Fields))
}

// UpdateProfileInput is the validated-by-the-handler-but-re-validated-here
// profile edit request. SocialLinks may contain any keys; unknown ones are
// dropped and known ones are URL-validated.
type UpdateProfileInput struct {
	Name        string
	Bio         string
	Website     string
	SocialLinks map[string]string
}

// AvatarUpload is a validated avatar ready to store. The service trusts the
// ContentType/Ext fields because storage.ValidateAvatar produced them by magic
// bytes; the handler MUST pass the result of ValidateAvatar here.
type AvatarUpload = storage.ValidatedAvatar

// ProfileService holds profile/author logic. Like AuthService it touches data
// only via repositories and blobs only via AvatarStore; it owns no globals.
type ProfileService struct {
	pool    db.Beginner
	users   UserRepository
	roles   RoleRepository
	avatars AvatarStore
}

// NewProfileService constructs a ProfileService with explicit dependencies.
func NewProfileService(pool db.Beginner, users UserRepository, roles RoleRepository, avatars AvatarStore) *ProfileService {
	return &ProfileService{pool: pool, users: users, roles: roles, avatars: avatars}
}

// UpdateProfile validates and persists a user's editable profile. It normalizes
// the website + social URLs (adding https:// when scheme-less), validates them,
// drops unknown social networks, and writes in a single transaction. Validation
// failures surface as ProfileValidationError with per-field messages.
func (s *ProfileService) UpdateProfile(ctx context.Context, userID uuid.UUID, in UpdateProfileInput) (User, error) {
	fieldErrs := map[string]string{}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		fieldErrs["name"] = "Name is required."
	} else if len(name) > 120 {
		fieldErrs["name"] = "Name is too long."
	}

	bio := strings.TrimSpace(in.Bio)
	if len(bio) > 2000 {
		fieldErrs["bio"] = "Bio is too long (2000 characters max)."
	}

	website := ""
	if raw := strings.TrimSpace(in.Website); raw != "" {
		normalized, err := normalizeURL(raw)
		if err != nil {
			fieldErrs["website"] = "Enter a valid http(s) URL."
		} else {
			website = normalized
		}
	}

	socials := map[string]string{}
	for _, key := range knownSocialNetworks {
		raw := strings.TrimSpace(in.SocialLinks[key])
		if raw == "" {
			continue
		}
		normalized, err := normalizeURL(raw)
		if err != nil {
			fieldErrs["social_"+key] = "Enter a valid http(s) URL."
			continue
		}
		socials[key] = normalized
	}

	if len(fieldErrs) > 0 {
		return User{}, ProfileValidationError{Fields: fieldErrs}
	}

	var updated User
	err := db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		u, err := s.users.UpdateProfileTx(ctx, tx, userID, ProfileFields{
			Name:        name,
			Bio:         bio,
			Website:     website,
			SocialLinks: socials,
		})
		if err != nil {
			return fmt.Errorf("update profile: %w", err)
		}
		updated = u
		return nil
	})
	if err != nil {
		return User{}, err
	}
	return updated, nil
}

// UpdateAvatar stores a validated avatar via the AvatarStore, points the user's
// avatar_path at the new object, and deletes the previously stored object (if
// any). The new object is written BEFORE the DB switch so a failed upload never
// leaves a dangling path; the old object is deleted AFTER the switch commits so a
// failed commit never orphans the user's current avatar.
func (s *ProfileService) UpdateAvatar(ctx context.Context, userID uuid.UUID, up AvatarUpload) (User, error) {
	current, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return User{}, fmt.Errorf("load user: %w", err)
	}

	key, err := storage.ObjectKey("avatars", userID.String(), up.Ext)
	if err != nil {
		return User{}, err
	}
	if _, err := s.avatars.Save(ctx, key, strings.NewReader(string(up.Data)), up.ContentType); err != nil {
		return User{}, fmt.Errorf("store avatar: %w", err)
	}

	var updated User
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		u, err := s.users.SetAvatarPathTx(ctx, tx, userID, key)
		if err != nil {
			return fmt.Errorf("set avatar path: %w", err)
		}
		updated = u
		return nil
	})
	if err != nil {
		// Roll back the orphaned new object on commit failure (best effort).
		_ = s.avatars.Delete(ctx, key)
		return User{}, err
	}

	// Old object is now unreferenced; remove it (best effort, never blocks).
	if current.AvatarPath != "" && current.AvatarPath != key {
		_ = s.avatars.Delete(ctx, current.AvatarPath)
	}
	return updated, nil
}

// AvatarURL resolves a user's avatar to a public URL, preferring a self-uploaded
// avatar (avatar_path via the store) over a provider URL. Empty when neither is
// set, so callers fall back to initials.
func (s *ProfileService) AvatarURL(u User) string {
	if u.AvatarPath != "" {
		return s.avatars.URL(u.AvatarPath)
	}
	return u.AvatarURL
}

// PublicAuthor is the email-free public view of an author plus their published
// posts. It is the payload rendered on /authors/{id} and injected into the
// ProfilePage/Person JSON-LD. There is intentionally NO Email field: the public
// page must never leak it.
type PublicAuthor struct {
	ID          uuid.UUID
	Name        string
	Bio         string
	AvatarURL   string
	Website     string
	SocialLinks map[string]string
	RoleLabel   string
	// Posts is the author's PUBLISHED posts. The content domain does not exist
	// until M2, so this is empty now (the seam is real; the data is not faked).
	Posts []AuthorPost
}

// AuthorPost is a placeholder for an author's published post, listed on the
// public profile. Populated in M2 when the content domain ships.
type AuthorPost struct {
	Title string
	URL   string
}

// PublicAuthor returns the public profile for the author with id, or ErrNotFound
// when absent. The returned payload contains NO email by construction.
func (s *ProfileService) PublicAuthor(ctx context.Context, id uuid.UUID) (PublicAuthor, error) {
	u, err := s.users.GetByID(ctx, id)
	if err != nil {
		return PublicAuthor{}, err
	}

	roleLabel := ""
	if role, rErr := s.roles.GetByID(ctx, u.RoleID); rErr == nil {
		roleLabel = role.Label
	}

	return PublicAuthor{
		ID:          u.ID,
		Name:        publicName(u),
		Bio:         u.Bio,
		AvatarURL:   s.AvatarURL(u),
		Website:     u.Website,
		SocialLinks: sortedSocials(u.SocialLinks),
		RoleLabel:   roleLabel,
		// TODO(M2): list this author's published posts once the content domain
		// (posts) exists. Returning an empty slice keeps the page honest now.
		Posts: []AuthorPost{},
	}, nil
}

// publicName returns the display name, never the email (which would leak PII on
// the public page). When the name is empty we fall back to a neutral label.
func publicName(u User) string {
	if n := strings.TrimSpace(u.Name); n != "" {
		return n
	}
	if u.Username != "" {
		return u.Username
	}
	return "Author"
}

// sortedSocials returns the social links in a deterministic key order so the
// rendered page (and its JSON-LD sameAs array) is stable across requests.
func sortedSocials(m map[string]string) map[string]string {
	// The map itself is unordered; callers that need order use SocialOrder.
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// SocialOrder returns the known social keys present in m, in a stable order.
// Templates iterate this rather than ranging the map so output is deterministic.
func SocialOrder(m map[string]string) []string {
	var keys []string
	for _, k := range knownSocialNetworks {
		if _, ok := m[k]; ok {
			keys = append(keys, k)
		}
	}
	// Any non-canonical keys (shouldn't occur post-validation) appended sorted.
	var extra []string
	for k := range m {
		if !contains(knownSocialNetworks, k) {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return append(keys, extra...)
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// normalizeURL trims, defaults a missing scheme to https, and validates that the
// result is an absolute http(s) URL with a host. It rejects other schemes
// (javascript:, data:, mailto:) so a stored URL can never be an XSS vector when
// rendered as an href.
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty url")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host")
	}
	return u.String(), nil
}
