package accounts

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrRoleNotFound is returned by the users-admin service when an update targets
// a role id that does not exist. The API layer maps it to a 422 validation
// error.
var ErrRoleNotFound = errors.New("accounts: role not found")

// ErrLastAdmin is returned when an update would demote the last remaining
// administrator, which would lock every operator out of the admin surface. The
// API layer maps it to a 409 conflict.
var ErrLastAdmin = errors.New("accounts: cannot demote the last administrator")

// userAdminUserRepo is the narrow user-repository surface the users-admin
// service needs: paginated listing, count, single load, and the admin field
// write. *UserRepoPG satisfies it.
type userAdminUserRepo interface {
	List(ctx context.Context, limit, offset int) ([]User, error)
	Count(ctx context.Context) (int, error)
	CountByRole(ctx context.Context, roleID uuid.UUID) (int, error)
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
	UpdateAdmin(ctx context.Context, id uuid.UUID, name string, roleID uuid.UUID) (User, error)
}

// userAdminRoleRepo is the narrow role-repository surface the users-admin
// service needs: list every role and resolve one by id (for validation).
// *RoleRepoPG satisfies it.
type userAdminRoleRepo interface {
	List(ctx context.Context) ([]Role, error)
	GetByID(ctx context.Context, id uuid.UUID) (Role, error)
}

// UserAdminService is the thin admin-facing service backing the REST Users area.
// It carries no ownership or self-service concerns (RBAC is enforced at the API
// route gate); it only lists/reads users and applies the admin-editable fields
// (name, role), validating the role exists before the write.
type UserAdminService struct {
	users userAdminUserRepo
	roles userAdminRoleRepo
}

// NewUserAdminService constructs a UserAdminService over the user and role
// repositories.
func NewUserAdminService(users userAdminUserRepo, roles userAdminRoleRepo) *UserAdminService {
	return &UserAdminService{users: users, roles: roles}
}

// ListUsers returns a page of users plus the total count for pagination.
func (s *UserAdminService) ListUsers(ctx context.Context, limit, offset int) ([]User, int, error) {
	items, err := s.users.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.users.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ListRoles returns every role (the admin role picker source).
func (s *UserAdminService) ListRoles(ctx context.Context) ([]Role, error) {
	return s.roles.List(ctx)
}

// GetUser loads a single user by id, returning ErrNotFound when absent.
func (s *UserAdminService) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	return s.users.GetByID(ctx, id)
}

// UpdateUser applies the admin-editable fields (name, role) to a user. It
// validates the role id exists first (returning ErrRoleNotFound otherwise) so an
// unknown role never lands on the row, and refuses to demote the last remaining
// administrator (ErrLastAdmin) so the admin surface can never be locked out.
func (s *UserAdminService) UpdateUser(ctx context.Context, id uuid.UUID, name string, roleID uuid.UUID) (User, error) {
	newRole, err := s.roles.GetByID(ctx, roleID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return User{}, ErrRoleNotFound
		}
		return User{}, err
	}

	// Last-administrator guard: if this change would move the target OUT of the
	// administrator role, ensure at least one other administrator remains.
	if newRole.Key != RoleAdministrator {
		target, err := s.users.GetByID(ctx, id)
		if err != nil {
			return User{}, err
		}
		curRole, err := s.roles.GetByID(ctx, target.RoleID)
		if err != nil {
			return User{}, err
		}
		if curRole.Key == RoleAdministrator {
			admins, err := s.users.CountByRole(ctx, target.RoleID)
			if err != nil {
				return User{}, err
			}
			if admins <= 1 {
				return User{}, ErrLastAdmin
			}
		}
	}

	return s.users.UpdateAdmin(ctx, id, name, roleID)
}
