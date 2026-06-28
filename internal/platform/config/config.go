// Package config loads typed application configuration from the environment.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds all runtime configuration for CMStack-Go. Every field is
// sourced from an environment variable so the app stays 12-factor friendly.
type Config struct {
	// AppEnv selects environment-specific behaviour (development|production|test).
	AppEnv string `env:"APP_ENV" envDefault:"development"`
	// HTTPAddr is the listen address for the HTTP server.
	HTTPAddr string `env:"HTTP_ADDR" envDefault:":8080"`
	// BaseURL is the externally reachable base URL used for absolute links.
	BaseURL string `env:"BASE_URL" envDefault:"http://localhost:8080"`
	// DatabaseURL is the Postgres DSN (required).
	DatabaseURL string `env:"DATABASE_URL,required"`
	// RedisURL is optional; used for caching/sessions when present.
	RedisURL string `env:"REDIS_URL" envDefault:""`
	// SessionKey is reserved for the future Postgres-backed session store and
	// cookie signing. The current scs in-memory store does not consume it, so it
	// is intentionally NOT required: a required-but-ignored secret is misleading.
	// TODO(M1): wire this into the persistent session store / cookie signer.
	SessionKey string `env:"SESSION_KEY" envDefault:""`
	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"15s"`
	// ReadTimeout / WriteTimeout bound HTTP request handling.
	ReadTimeout  time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`

	// SignupEnabled toggles public registration. Read by the auth
	// SettingsProvider; the admin-UI-backed source arrives in M15.
	SignupEnabled bool `env:"SIGNUP_ENABLED" envDefault:"true"`
	// EmailVerificationRequired enforces a verified email before login.
	EmailVerificationRequired bool `env:"EMAIL_VERIFICATION_REQUIRED" envDefault:"false"`

	// AdminEmail / AdminPassword seed the default administrator. The password
	// default is for local development only and MUST be overridden in production.
	AdminEmail    string `env:"ADMIN_EMAIL" envDefault:"admin@cmstack.local"`
	AdminPassword string `env:"ADMIN_PASSWORD" envDefault:"changeme-admin-password"`

	// UploadDir is the filesystem root for user uploads (avatars now; richer media
	// in M4). Served read-only at /uploads with a sniff-proof handler.
	UploadDir string `env:"UPLOAD_DIR" envDefault:"./uploads"`

	// OAuth (social login, M1-ext). A provider is offered ONLY when its client
	// id+secret are both present; absent keys mean the provider is silently not
	// offered (graceful no-op, like reCAPTCHA). OAuthCallbackBase is the external
	// base used to build the provider callback URL; it defaults to BaseURL when
	// empty (resolved in OAuthProviders()).
	OAuthCallbackBase  string `env:"OAUTH_CALLBACK_BASE" envDefault:""`
	GoogleClientID     string `env:"OAUTH_GOOGLE_CLIENT_ID" envDefault:""`
	GoogleClientSecret string `env:"OAUTH_GOOGLE_CLIENT_SECRET" envDefault:""`
	GitHubClientID     string `env:"OAUTH_GITHUB_CLIENT_ID" envDefault:""`
	GitHubClientSecret string `env:"OAUTH_GITHUB_CLIENT_SECRET" envDefault:""`
}

// IsProduction reports whether the app runs in production mode.
func (c Config) IsProduction() bool { return c.AppEnv == "production" }

// IsDevelopment reports whether the app runs in development mode.
func (c Config) IsDevelopment() bool { return c.AppEnv == "development" }

// Load parses the environment into a Config, returning an error when a required
// variable is missing or a value cannot be parsed. Duration values are parsed
// (and validated) by env's built-in time.Duration support, so a malformed
// duration such as "15x" fails fast here instead of silently falling back.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
