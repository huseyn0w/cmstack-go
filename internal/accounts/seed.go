package accounts

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
)

// AllSubjects is the canonical set of authorization subjects.
var AllSubjects = []string{
	SubjectPost, SubjectPage, SubjectService, SubjectMedia, SubjectComment,
	SubjectUser, SubjectSetting, SubjectSEO, SubjectMenu, SubjectTheme, SubjectPlugin,
}

// AllActions is the canonical set of authorization actions.
var AllActions = []string{
	ActionCreate, ActionRead, ActionUpdate, ActionDelete, ActionPublish, ActionManage,
}

// roleSeed is one role and the permission set it grants.
type roleSeed struct {
	Key         string
	Label       string
	Permissions []Permission
}

// canonicalRoles is the single source of truth for the four built-in roles and
// their grants, exactly as specified in M1-core. Administrator gets manage:all;
// the rest get scoped grants.
func canonicalRoles() []roleSeed {
	editor := []Permission{
		{ActionManage, SubjectPost},
		{ActionManage, SubjectPage},
		{ActionManage, SubjectService},
		{ActionManage, SubjectMedia},
		{ActionManage, SubjectComment},
		{ActionManage, SubjectSEO},
		{ActionManage, SubjectMenu},
		{ActionRead, SubjectUser},
	}
	author := []Permission{
		// create + update + publish + delete OWN post + media, read others, create
		// comment. Ownership scoping is enforced at the SERVICE layer (the gate):
		// these coarse grants let an Author act on posts, and the post service
		// additionally restricts publish/update/delete to the author's OWN posts
		// (Editor/Administrator are unrestricted). See posts.Service ownership gate.
		{ActionCreate, SubjectPost},
		{ActionUpdate, SubjectPost},
		{ActionRead, SubjectPost},
		{ActionPublish, SubjectPost},
		{ActionDelete, SubjectPost},
		{ActionCreate, SubjectMedia},
		{ActionUpdate, SubjectMedia},
		{ActionRead, SubjectMedia},
		{ActionCreate, SubjectComment},
	}
	member := []Permission{
		{ActionRead, SubjectPost},
		{ActionRead, SubjectPage},
		{ActionRead, SubjectService},
		{ActionCreate, SubjectComment},
	}
	return []roleSeed{
		{RoleAdministrator, "Administrator", []Permission{{ActionManage, SubjectAll}}},
		{RoleEditor, "Editor", editor},
		{RoleAuthor, "Author", author},
		{RoleMember, "Member", member},
	}
}

// allPermissions returns every (action, subject) pair that must exist, including
// manage:all, so the permissions table is fully populated regardless of which
// pairs a role references.
func allPermissions() []Permission {
	perms := []Permission{{ActionManage, SubjectAll}}
	for _, subj := range AllSubjects {
		for _, act := range AllActions {
			perms = append(perms, Permission{Action: act, Subject: subj})
		}
	}
	return perms
}

// Seeder seeds roles, permissions, their mappings, and a default administrator.
// It is idempotent: re-running upserts roles/permissions and re-grants without
// creating duplicates, and only creates the admin if it does not already exist.
type Seeder struct {
	pool   db.Beginner
	q      *sqlcgen.Queries
	users  UserRepository
	roles  RoleRepository
	hasher Hasher
}

// NewSeeder constructs a Seeder.
func NewSeeder(pool db.Beginner, q *sqlcgen.Queries, users UserRepository, roles RoleRepository, hasher Hasher) *Seeder {
	return &Seeder{pool: pool, q: q, users: users, roles: roles, hasher: hasher}
}

// AdminSeed is the default administrator account to ensure exists.
type AdminSeed struct {
	Email    string
	Password string
	Name     string
}

// Seed runs the full idempotent seed in a single transaction.
func (s *Seeder) Seed(ctx context.Context, admin AdminSeed) error {
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		// 1. Upsert every permission, recording its id by (action,subject).
		permID := make(map[Permission]sqlcgen.Permission, len(allPermissions()))
		for _, p := range allPermissions() {
			row, err := q.UpsertPermission(ctx, sqlcgen.UpsertPermissionParams{Action: p.Action, Subject: p.Subject})
			if err != nil {
				return fmt.Errorf("upsert permission %s:%s: %w", p.Action, p.Subject, err)
			}
			permID[p] = row
		}

		// 2. Upsert roles and grant their permissions.
		var adminRoleID sqlcgen.Role
		for _, rs := range canonicalRoles() {
			role, err := q.UpsertRole(ctx, sqlcgen.UpsertRoleParams{Key: rs.Key, Label: rs.Label})
			if err != nil {
				return fmt.Errorf("upsert role %s: %w", rs.Key, err)
			}
			if rs.Key == RoleAdministrator {
				adminRoleID = role
			}
			for _, p := range rs.Permissions {
				perm, ok := permID[p]
				if !ok {
					return fmt.Errorf("role %s references unknown permission %s:%s", rs.Key, p.Action, p.Subject)
				}
				if err := q.GrantPermission(ctx, sqlcgen.GrantPermissionParams{
					RoleID:       role.ID,
					PermissionID: perm.ID,
				}); err != nil {
					return fmt.Errorf("grant %s:%s to %s: %w", p.Action, p.Subject, rs.Key, err)
				}
			}
		}

		// 3. Ensure the default administrator exists (idempotent: skip if present).
		count, err := q.CountUsersByEmail(ctx, admin.Email)
		if err != nil {
			return fmt.Errorf("count admin: %w", err)
		}
		if count == 0 {
			hash, err := s.hasher.Hash(admin.Password)
			if err != nil {
				return fmt.Errorf("hash admin password: %w", err)
			}
			name := admin.Name
			if name == "" {
				name = "Administrator"
			}
			if _, err := s.users.CreateTx(ctx, tx, CreateUserInput{
				Email:           admin.Email,
				PasswordHash:    hash,
				Name:            name,
				RoleID:          fromPgUUID(adminRoleID.ID),
				EmailVerifiedAt: nil,
			}); err != nil {
				return fmt.Errorf("create admin: %w", err)
			}
			// Stamp the admin as verified so it can log in even when verification
			// is required. We do this via the verified-at column on insert path:
			// re-fetch and mark verified within the same tx.
			admEmail, err := q.GetUserByEmail(ctx, admin.Email)
			if err != nil {
				return fmt.Errorf("reload admin: %w", err)
			}
			if err := q.MarkEmailVerified(ctx, admEmail.ID); err != nil {
				return fmt.Errorf("verify admin: %w", err)
			}
		}
		return nil
	})
}
