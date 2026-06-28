package accounts

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// Action constants. manage implies every other action on a subject.
const (
	ActionCreate  = "create"
	ActionRead    = "read"
	ActionUpdate  = "update"
	ActionDelete  = "delete"
	ActionPublish = "publish"
	ActionManage  = "manage"
)

// Subject constants — the resource an action applies to. SubjectAll grants a
// permission across every subject (used by Administrator's manage:all).
const (
	SubjectAll     = "all"
	SubjectPost    = "post"
	SubjectPage    = "page"
	SubjectService = "service"
	SubjectMedia   = "media"
	SubjectComment = "comment"
	SubjectUser    = "user"
	SubjectSetting = "setting"
	SubjectSEO     = "seo"
	SubjectMenu    = "menu"
	SubjectTheme   = "theme"
	SubjectPlugin  = "plugin"
)

// roleLoader is the narrow dependency the Authorizer needs to load a user's
// role key and the role->permissions map. UserRepository + RoleRepository
// satisfy it; the authorizer caches the permission map and invalidates on
// change. It is the single source of truth for authorization decisions.
type roleLoader interface {
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
}

// Authorizer answers Can(user, action, subject) by loading role->permissions
// from the DB once and caching them in-process. Invalidate() clears the cache
// when role_permissions change (admin UI = M15 will call it).
type Authorizer struct {
	users roleLoader
	roles RoleRepository

	mu     sync.RWMutex
	byRole map[string][]Permission // role key -> permissions
	loaded bool
}

// NewAuthorizer constructs an Authorizer over the user and role repositories.
func NewAuthorizer(users roleLoader, roles RoleRepository) *Authorizer {
	return &Authorizer{
		users: users,
		roles: roles,
	}
}

// Invalidate clears the cached role->permission map so the next Can reloads it.
// Call this whenever role_permissions change.
func (a *Authorizer) Invalidate() {
	a.mu.Lock()
	a.byRole = nil
	a.loaded = false
	a.mu.Unlock()
}

func (a *Authorizer) ensureLoaded(ctx context.Context) error {
	a.mu.RLock()
	loaded := a.loaded
	a.mu.RUnlock()
	if loaded {
		return nil
	}
	m, err := a.roles.AllRolePermissions(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.byRole = m
	a.loaded = true
	a.mu.Unlock()
	return nil
}

// CanRole reports whether a role key grants (action, subject). This is the pure
// decision used by Can and is directly table-testable.
func (a *Authorizer) CanRole(ctx context.Context, roleKey, action, subject string) (bool, error) {
	if err := a.ensureLoaded(ctx); err != nil {
		return false, err
	}
	a.mu.RLock()
	perms := a.byRole[roleKey]
	a.mu.RUnlock()
	return grants(perms, action, subject), nil
}

// Can reports whether the user identified by userID may perform action on
// subject. A load error or unknown user yields false (deny-by-default).
func (a *Authorizer) Can(ctx context.Context, userID uuid.UUID, action, subject string) bool {
	u, err := a.users.GetByID(ctx, userID)
	if err != nil {
		return false
	}
	role, err := a.roles.GetByID(ctx, u.RoleID)
	if err != nil {
		return false
	}
	ok, err := a.CanRole(ctx, role.Key, action, subject)
	if err != nil {
		return false
	}
	return ok
}

// grants implements the permission-matching rules:
//   - an exact (action, subject) match grants;
//   - manage on the subject grants any action on that subject;
//   - a permission on SubjectAll grants the action on every subject;
//   - manage:all grants everything.
func grants(perms []Permission, action, subject string) bool {
	for _, p := range perms {
		actionOK := p.Action == action || p.Action == ActionManage
		subjectOK := p.Subject == subject || p.Subject == SubjectAll
		if actionOK && subjectOK {
			return true
		}
	}
	return false
}
