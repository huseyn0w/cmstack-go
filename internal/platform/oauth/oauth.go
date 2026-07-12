// Package oauth configures the goth social-login providers from typed config.
// Providers are wired ONLY when their client id+secret are both present, so an
// unconfigured provider is simply not offered (a graceful no-op). gothic's
// session store is a gorilla cookie store keyed off the application SESSION_KEY,
// so OAuth state/CSRF survives the round trip to the provider without coupling
// to the scs application session.
package oauth

import (
	"crypto/sha256"
	"net/http"
	"strings"

	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/github"
	"github.com/markbates/goth/providers/google"
)

// Provider names. These are the goth provider keys AND the values stored in
// oauth_accounts.provider, so they must stay stable.
const (
	ProviderGoogle = "google"
	ProviderGitHub = "github"
)

// Provider describes one enabled social-login provider for the UI (button
// label + the begin/callback paths).
type Provider struct {
	Name  string // goth key, e.g. "google"
	Label string // human label, e.g. "Google"
}

// Config carries the resolved OAuth settings needed to register providers.
type Config struct {
	// CallbackBase is the external base URL used to build callback URLs, e.g.
	// "https://example.com". The callback path is "/auth/{provider}/callback".
	CallbackBase string
	// SessionKey signs gothic's state cookie. When empty a process-stable key is
	// derived so dev still works (state is short-lived); production MUST set it.
	SessionKey string
	Production bool

	GoogleClientID     string
	GoogleClientSecret string
	GitHubClientID     string
	GitHubClientSecret string
}

// Setup registers the configured providers with goth and installs gothic's
// session store. It returns the list of enabled providers (for the UI) and is a
// no-op-friendly: with no credentials it registers nothing and returns an empty
// slice. Call once during wiring.
func Setup(cfg Config) []Provider {
	gothic.Store = newStore(cfg.SessionKey, cfg.Production)

	base := strings.TrimRight(cfg.CallbackBase, "/")
	var (
		providers []goth.Provider
		enabled   []Provider
	)

	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		providers = append(providers, google.New(
			cfg.GoogleClientID, cfg.GoogleClientSecret,
			base+"/auth/"+ProviderGoogle+"/callback",
			"email", "profile",
		))
		enabled = append(enabled, Provider{Name: ProviderGoogle, Label: "Google"})
	}

	if cfg.GitHubClientID != "" && cfg.GitHubClientSecret != "" {
		providers = append(providers, github.New(
			cfg.GitHubClientID, cfg.GitHubClientSecret,
			base+"/auth/"+ProviderGitHub+"/callback",
			"user:email",
		))
		enabled = append(enabled, Provider{Name: ProviderGitHub, Label: "GitHub"})
	}

	goth.UseProviders(providers...)
	return enabled
}

// newStore builds the gorilla cookie store gothic uses for the OAuth state
// round trip. The key is derived from SessionKey via sha-256 so any
// (possibly short) configured secret yields a fixed 32-byte signing key; an
// empty SessionKey falls back to a constant dev key (state cookies are
// short-lived and only guard the OAuth handshake).
func newStore(sessionKey string, production bool) sessions.Store {
	seed := sessionKey
	if seed == "" {
		seed = "agentic-cms-oauth-dev-key"
	}
	sum := sha256.Sum256([]byte(seed))
	st := sessions.NewCookieStore(sum[:])
	st.Options.HttpOnly = true
	st.Options.Secure = production
	st.Options.SameSite = http.SameSiteLaxMode
	st.Options.Path = "/"
	st.MaxAge(600) // OAuth handshake is short-lived: 10 minutes.
	return st
}
