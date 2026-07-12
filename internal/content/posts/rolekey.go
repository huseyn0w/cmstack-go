package posts

import (
	"context"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
)

// userByID resolves a user's role id. *accounts.UserRepoPG satisfies it.
type userByID interface {
	GetByID(ctx context.Context, id uuid.UUID) (accounts.User, error)
}

// roleByID resolves a role's key by id. *accounts.RoleRepoPG satisfies it.
type roleByID interface {
	GetByID(ctx context.Context, id uuid.UUID) (accounts.Role, error)
}

// RoleKeyResolver adapts the accounts user+role repos to the posts service's
// UserRoleResolver port: it maps a user id to its role key so the ownership gate
// can recognize privileged roles (Editor/Administrator) without the posts
// package depending on the accounts repos' concrete types.
type RoleKeyResolver struct {
	users userByID
	roles roleByID
}

// NewRoleKeyResolver constructs the adapter.
func NewRoleKeyResolver(users userByID, roles roleByID) *RoleKeyResolver {
	return &RoleKeyResolver{users: users, roles: roles}
}

// RoleKey returns the role key for userID.
func (r *RoleKeyResolver) RoleKey(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := r.users.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	role, err := r.roles.GetByID(ctx, u.RoleID)
	if err != nil {
		return "", err
	}
	return role.Key, nil
}
