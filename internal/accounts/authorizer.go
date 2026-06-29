package accounts

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// defaultPermissionCacheTTL bounds how long a cached role->permission map may be
// served before it is reloaded. It is a safety net so role_permission edits
// self-heal without a process restart even before the M15 admin UI wires
// Invalidate(). 60s keeps stale grants short-lived while preserving the cache's
// per-request benefit.
const defaultPermissionCacheTTL = 60 * time.Second

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
	// SubjectCategory gates the taxonomy category admin (M3). Categories are a
	// site-wide tree (no per-author ownership), so the coarse grant alone gates
	// every action.
	SubjectCategory = "category"
	// SubjectTag gates the taxonomy tag admin (M3). Tags are flat and site-wide;
	// like categories they are treated as a distinct subject so an Editor can be
	// granted taxonomy management independently of posts.
	SubjectTag     = "tag"
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

	ttl time.Duration
	now func() time.Time

	mu       sync.RWMutex
	byRole   map[string][]Permission // role key -> permissions
	loaded   bool
	loadedAt time.Time
}

// NewAuthorizer constructs an Authorizer over the user and role repositories
// with the default safety TTL (defaultPermissionCacheTTL) and the wall clock.
func NewAuthorizer(users roleLoader, roles RoleRepository) *Authorizer {
	return NewAuthorizerWithTTL(users, roles, defaultPermissionCacheTTL, time.Now)
}

// NewAuthorizerWithTTL constructs an Authorizer with an explicit cache TTL and
// clock. A non-positive ttl disables time-based expiry (cache only busts via
// Invalidate); the clock is injectable for deterministic tests. A nil clock
// defaults to time.Now.
func NewAuthorizerWithTTL(users roleLoader, roles RoleRepository, ttl time.Duration, now func() time.Time) *Authorizer {
	if now == nil {
		now = time.Now
	}
	return &Authorizer{
		users: users,
		roles: roles,
		ttl:   ttl,
		now:   now,
	}
}

// Invalidate clears the cached role->permission map so the next Can reloads it.
// Call this whenever role_permissions change.
func (a *Authorizer) Invalidate() {
	a.mu.Lock()
	a.byRole = nil
	a.loaded = false
	a.loadedAt = time.Time{}
	a.mu.Unlock()
}

func (a *Authorizer) ensureLoaded(ctx context.Context) error {
	a.mu.RLock()
	fresh := a.loaded && !a.expiredLocked()
	a.mu.RUnlock()
	if fresh {
		return nil
	}
	m, err := a.roles.AllRolePermissions(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.byRole = m
	a.loaded = true
	a.loadedAt = a.now()
	a.mu.Unlock()
	return nil
}

// expiredLocked reports whether the cached map has outlived its TTL. Callers
// must hold at least the read lock. A non-positive TTL never expires.
func (a *Authorizer) expiredLocked() bool {
	if a.ttl <= 0 {
		return false
	}
	return a.now().Sub(a.loadedAt) >= a.ttl
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
