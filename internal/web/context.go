package web

import (
	"context"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
)

// ctxKey is an unexported context key type to avoid collisions.
type ctxKey int

const userCtxKey ctxKey = iota

// withUser returns a copy of ctx carrying the authenticated user.
func withUser(ctx context.Context, u accounts.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// UserFromContext returns the authenticated user and whether one is present.
func UserFromContext(ctx context.Context) (accounts.User, bool) {
	u, ok := ctx.Value(userCtxKey).(accounts.User)
	return u, ok
}
