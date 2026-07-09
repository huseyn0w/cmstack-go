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
	// HTTPAddr is the listen address for the HTTP server. Default :8090 keeps the
	// Go stack off :8080 so it can run alongside the sibling cmstack-* projects
	// during local dev (see ../../../../PORTS.md for the cross-stack allocation).
	HTTPAddr string `env:"HTTP_ADDR" envDefault:":8090"`
	// BaseURL is the externally reachable base URL used for absolute links.
	BaseURL string `env:"BASE_URL" envDefault:"http://localhost:8090"`
	// DatabaseURL is the Postgres DSN (required).
	DatabaseURL string `env:"DATABASE_URL,required"`
	// RedisURL is optional; used for caching/sessions when present
	// (e.g. redis://localhost:6379/0). Required only when CacheDriver=redis.
	RedisURL string `env:"REDIS_URL" envDefault:""`
	// CacheDriver selects the cache backend (M13): "memory" (default, in-process),
	// "redis" (shared, uses RedisURL) or "noop" (caching disabled).
	CacheDriver string `env:"CACHE_DRIVER" envDefault:"memory"`
	// CacheKeyPrefix namespaces every Redis cache key so a Redis instance can be
	// shared safely; Clear/DeleteByPrefix stay scoped to this prefix.
	CacheKeyPrefix string `env:"CACHE_KEY_PREFIX" envDefault:"cmstack:"`
	// CachePageTTL bounds how long an anonymous public page response is cached
	// (M13-2). It is a backstop: the page cache is also invalidated eagerly on
	// content publish. A short default keeps silent edits (which emit no publish
	// event) fresh within the window.
	CachePageTTL time.Duration `env:"CACHE_PAGE_TTL" envDefault:"60s"`
	// CacheMenuTTL bounds how long a resolved menu tree is cached (M13-2). It is a
	// backstop: the menu cache is invalidated eagerly on any menu mutation.
	CacheMenuTTL time.Duration `env:"CACHE_MENU_TTL" envDefault:"10m"`
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

	// UploadDir is the filesystem root for user uploads (avatars + the M4 media
	// library) when STORAGE_DRIVER=local. Served read-only at /uploads with a
	// sniff-proof handler.
	UploadDir string `env:"UPLOAD_DIR" envDefault:"./uploads"`

	// Storage driver selection (M4). "local" (default) writes under UploadDir and
	// serves /uploads; "s3" stores objects in an S3 (or S3-compatible: MinIO,
	// Cloudflare R2) bucket configured by the S3_* vars below.
	StorageDriver string `env:"STORAGE_DRIVER" envDefault:"local"`
	// MediaMaxBytes caps a single media upload (default 10 MiB). Enforced on bytes
	// actually read, not a client Content-Length.
	MediaMaxBytes int64 `env:"MEDIA_MAX_BYTES" envDefault:"10485760"`
	// S3 storage config (used only when STORAGE_DRIVER=s3). Credentials may be
	// empty to use the AWS default credential chain (env/shared config/IAM role).
	// Endpoint+PathStyle target S3-compatible providers; PublicBaseURL is an
	// optional CDN/website base for object URLs.
	S3Bucket        string `env:"S3_BUCKET" envDefault:""`
	S3Region        string `env:"S3_REGION" envDefault:""`
	S3Endpoint      string `env:"S3_ENDPOINT" envDefault:""`
	S3AccessKeyID   string `env:"S3_ACCESS_KEY_ID" envDefault:""`
	S3SecretKey     string `env:"S3_SECRET_ACCESS_KEY" envDefault:""`
	S3UsePathStyle  bool   `env:"S3_USE_PATH_STYLE" envDefault:"false"`
	S3PublicBaseURL string `env:"S3_PUBLIC_BASE_URL" envDefault:""`

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

	// reCAPTCHA v3 (M5 comments anti-spam). The secret is OPTIONAL: when empty the
	// verifier is a graceful no-op (guest comments work without keys, mirroring the
	// reference stacks); when set, a guest submission must carry a token whose
	// score meets RecaptchaMinScore. RecaptchaSiteKey is exposed to the public form
	// to fetch the token client-side.
	RecaptchaSecret   string  `env:"RECAPTCHA_SECRET" envDefault:""`
	RecaptchaSiteKey  string  `env:"RECAPTCHA_SITE_KEY" envDefault:""`
	RecaptchaMinScore float64 `env:"RECAPTCHA_MIN_SCORE" envDefault:"0.5"`

	// CommentRateLimitPerMinute caps guest/member comment submissions per client
	// IP (M5). Defaults to 8/min (ts parity).
	CommentRateLimitPerMinute float64 `env:"COMMENT_RATE_LIMIT_PER_MINUTE" envDefault:"8"`

	// ContactRecipient is the fallback recipient for the public contact form (M12)
	// when the settings key `contact_recipient` is unset/empty. When this is also
	// empty the notifier falls back to AdminEmail.
	ContactRecipient string `env:"CONTACT_RECIPIENT" envDefault:""`

	// Email / notifications (M14). MailDriver selects the transactional-email
	// backend: "log" (default, dev — logs the links), "smtp" (real delivery) or
	// "noop" (silent, for tests/CI). The SMTP_* vars configure the smtp driver;
	// MailFrom defaults to AdminEmail when empty. SMTPTLS selects the transport
	// security: "starttls" (default, upgrade on 587), "tls" (implicit TLS, 465)
	// or "none" (unencrypted — dev/relay only).
	MailDriver   string `env:"MAIL_DRIVER" envDefault:"log"`
	SMTPHost     string `env:"SMTP_HOST" envDefault:""`
	SMTPPort     int    `env:"SMTP_PORT" envDefault:"587"`
	SMTPUsername string `env:"SMTP_USERNAME" envDefault:""`
	SMTPPassword string `env:"SMTP_PASSWORD" envDefault:""`
	MailFrom     string `env:"MAIL_FROM" envDefault:""`
	MailFromName string `env:"MAIL_FROM_NAME" envDefault:"CMStack"`
	SMTPTLS      string `env:"SMTP_TLS" envDefault:"starttls"`

	// SEO / site identity (M8). All optional with sensible defaults so the app
	// runs without any of these set; they enrich the document head + (later)
	// JSON-LD Organization. SiteName is also passed as a router Dep today; it is
	// mirrored here so the site identity lives in one config surface.
	SiteName        string `env:"SITE_NAME" envDefault:"CMStack"`
	SiteDescription string `env:"SITE_DESCRIPTION" envDefault:""`
	// DefaultOGImage is the OG/Twitter image fallback (absolute URL or a rooted
	// path resolved against BaseURL).
	DefaultOGImage string `env:"DEFAULT_OG_IMAGE" envDefault:""`
	// TwitterHandle is the site's Twitter/X handle (e.g. "@cmstack").
	TwitterHandle string `env:"TWITTER_HANDLE" envDefault:""`
	// GlobalNoindex forces noindex site-wide (a staging gate).
	GlobalNoindex bool `env:"GLOBAL_NOINDEX" envDefault:"false"`
	// AllowAICrawlers is consumed by the later robots slice; the identity lives
	// here so the whole site-identity surface is in one place.
	AllowAICrawlers bool `env:"ALLOW_AI_CRAWLERS" envDefault:"true"`

	// Search-engine verification tokens. Each emits a <meta name=... content=...>
	// verification tag in the head when set.
	GoogleSiteVerification string `env:"GOOGLE_SITE_VERIFICATION" envDefault:""`
	BingSiteVerification   string `env:"BING_SITE_VERIFICATION" envDefault:""`
	YandexVerification     string `env:"YANDEX_VERIFICATION" envDefault:""`
	PinterestVerification  string `env:"PINTEREST_VERIFICATION" envDefault:""`

	// GEO / business identity (consumed later by JSON-LD Organization; defined
	// now so the site identity lives in one place). SameAs is the list of social
	// profile URLs.
	OrgName       string   `env:"ORG_NAME" envDefault:""`
	OrgLegalName  string   `env:"ORG_LEGAL_NAME" envDefault:""`
	OrgLogo       string   `env:"ORG_LOGO" envDefault:""`
	OrgEmail      string   `env:"ORG_EMAIL" envDefault:""`
	OrgPhone      string   `env:"ORG_PHONE" envDefault:""`
	OrgStreet     string   `env:"ORG_STREET" envDefault:""`
	OrgLocality   string   `env:"ORG_LOCALITY" envDefault:""`
	OrgRegion     string   `env:"ORG_REGION" envDefault:""`
	OrgPostalCode string   `env:"ORG_POSTAL_CODE" envDefault:""`
	OrgCountry    string   `env:"ORG_COUNTRY" envDefault:""`
	GeoStatement  string   `env:"GEO_STATEMENT" envDefault:""`
	SameAs        []string `env:"SOCIAL_PROFILES" envSeparator:","`
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
