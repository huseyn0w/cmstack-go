package contact

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// Verifier verifies an OPTIONAL anti-spam token (reCAPTCHA v3). The platform
// recaptcha.Verifier satisfies it; when no secret is configured Verify returns
// (true, nil) so the contact form works without keys (graceful no-op). Declaring
// the narrow interface here keeps contact decoupled from the recaptcha package
// and trivially fakeable.
type Verifier interface {
	Verify(ctx context.Context, token string) (bool, error)
}

// RateLimiter answers whether an action for a key (the client IP) may proceed,
// consuming a token. *ratelimit.Limiter satisfies it.
type RateLimiter interface {
	Allow(key string) bool
}

// Publisher publishes a domain event inside a transaction. *events.Bus
// satisfies it.
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, event events.Event) error
}
