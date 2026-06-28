package accounts

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// seedPermissions mirrors the documented role->permission mapping so the
// authorizer is tested against the real policy, not an ad-hoc one.
func seedPermissions() map[string][]Permission {
	return map[string][]Permission{
		RoleAdministrator: {{Action: ActionManage, Subject: SubjectAll}},
		RoleEditor: {
			{Action: ActionManage, Subject: SubjectPost},
			{Action: ActionManage, Subject: SubjectPage},
			{Action: ActionManage, Subject: SubjectService},
			{Action: ActionManage, Subject: SubjectMedia},
			{Action: ActionManage, Subject: SubjectComment},
			{Action: ActionManage, Subject: SubjectSEO},
			{Action: ActionManage, Subject: SubjectMenu},
			{Action: ActionRead, Subject: SubjectUser},
		},
		RoleAuthor: {
			{Action: ActionCreate, Subject: SubjectPost},
			{Action: ActionUpdate, Subject: SubjectPost},
			{Action: ActionRead, Subject: SubjectPost},
			{Action: ActionCreate, Subject: SubjectMedia},
			{Action: ActionRead, Subject: SubjectMedia},
			{Action: ActionCreate, Subject: SubjectComment},
		},
		RoleMember: {
			{Action: ActionRead, Subject: SubjectPost},
			{Action: ActionRead, Subject: SubjectPage},
			{Action: ActionCreate, Subject: SubjectComment},
		},
	}
}

type stubRoleRepo struct {
	perms map[string][]Permission
	calls int // counts AllRolePermissions invocations (to prove caching)
}

func (s *stubRoleRepo) GetByKey(context.Context, string) (Role, error) { return Role{}, ErrNotFound }

func (s *stubRoleRepo) GetByID(context.Context, uuid.UUID) (Role, error) {
	return Role{}, ErrNotFound
}

func (s *stubRoleRepo) AllRolePermissions(context.Context) (map[string][]Permission, error) {
	s.calls++
	return s.perms, nil
}

type stubUserRepo struct {
	users map[uuid.UUID]User
}

func (s stubUserRepo) GetByID(_ context.Context, id uuid.UUID) (User, error) {
	if u, ok := s.users[id]; ok {
		return u, nil
	}
	return User{}, ErrNotFound
}

func TestAuthorizerCanRoleMatrix(t *testing.T) {
	roles := &stubRoleRepo{perms: seedPermissions()}
	a := NewAuthorizer(stubUserRepo{}, roles)

	tests := []struct {
		role    string
		action  string
		subject string
		want    bool
	}{
		// Administrator: manage:all grants everything.
		{RoleAdministrator, ActionDelete, SubjectUser, true},
		{RoleAdministrator, ActionManage, SubjectSetting, true},
		{RoleAdministrator, ActionPublish, SubjectTheme, true},
		// Editor: manages content subjects, read-only on user, none on setting.
		{RoleEditor, ActionPublish, SubjectPost, true},
		{RoleEditor, ActionManage, SubjectComment, true},
		{RoleEditor, ActionRead, SubjectUser, true},
		{RoleEditor, ActionDelete, SubjectUser, false},
		{RoleEditor, ActionUpdate, SubjectSetting, false},
		{RoleEditor, ActionManage, SubjectTheme, false},
		// Author: create/update/read post + media, create comment; cannot delete or publish.
		{RoleAuthor, ActionCreate, SubjectPost, true},
		{RoleAuthor, ActionUpdate, SubjectPost, true},
		{RoleAuthor, ActionRead, SubjectPost, true},
		{RoleAuthor, ActionDelete, SubjectPost, false},
		{RoleAuthor, ActionPublish, SubjectPost, false},
		{RoleAuthor, ActionCreate, SubjectComment, true},
		{RoleAuthor, ActionManage, SubjectMedia, false},
		{RoleAuthor, ActionUpdate, SubjectMedia, false},
		// Member: read + create comment only.
		{RoleMember, ActionRead, SubjectPost, true},
		{RoleMember, ActionCreate, SubjectComment, true},
		{RoleMember, ActionCreate, SubjectPost, false},
		{RoleMember, ActionUpdate, SubjectComment, false},
		{RoleMember, ActionManage, SubjectMedia, false},
		// Unknown role grants nothing.
		{"ghost", ActionRead, SubjectPost, false},
	}

	for _, tt := range tests {
		got, err := a.CanRole(context.Background(), tt.role, tt.action, tt.subject)
		if err != nil {
			t.Fatalf("CanRole(%s,%s,%s): %v", tt.role, tt.action, tt.subject, err)
		}
		if got != tt.want {
			t.Errorf("CanRole(%s, %s, %s) = %v, want %v", tt.role, tt.action, tt.subject, got, tt.want)
		}
	}
}

func TestAuthorizerCachesAndInvalidates(t *testing.T) {
	roles := &stubRoleRepo{perms: seedPermissions()}
	a := NewAuthorizer(stubUserRepo{}, roles)

	for i := 0; i < 5; i++ {
		if _, err := a.CanRole(context.Background(), RoleMember, ActionRead, SubjectPost); err != nil {
			t.Fatal(err)
		}
	}
	if roles.calls != 1 {
		t.Fatalf("expected permissions loaded once (cached), got %d loads", roles.calls)
	}

	a.Invalidate()
	if _, err := a.CanRole(context.Background(), RoleMember, ActionRead, SubjectPost); err != nil {
		t.Fatal(err)
	}
	if roles.calls != 2 {
		t.Fatalf("expected reload after Invalidate, got %d loads", roles.calls)
	}
}

func TestAuthorizerCanResolvesUserRole(t *testing.T) {
	adminRoleID := uuid.New()
	memberRoleID := uuid.New()
	adminID := uuid.New()
	memberID := uuid.New()

	users := stubUserRepo{users: map[uuid.UUID]User{
		adminID:  {ID: adminID, RoleID: adminRoleID},
		memberID: {ID: memberID, RoleID: memberRoleID},
	}}
	roles := &roleByIDStub{
		byID:  map[uuid.UUID]Role{adminRoleID: {ID: adminRoleID, Key: RoleAdministrator}, memberRoleID: {ID: memberRoleID, Key: RoleMember}},
		perms: seedPermissions(),
	}
	a := NewAuthorizer(users, roles)

	if !a.Can(context.Background(), adminID, ActionDelete, SubjectUser) {
		t.Error("administrator should be able to delete user")
	}
	if a.Can(context.Background(), memberID, ActionDelete, SubjectUser) {
		t.Error("member should NOT be able to delete user")
	}
	if a.Can(context.Background(), uuid.New(), ActionRead, SubjectPost) {
		t.Error("unknown user must be denied (deny-by-default)")
	}
}

type roleByIDStub struct {
	byID  map[uuid.UUID]Role
	perms map[string][]Permission
}

func (s *roleByIDStub) GetByKey(context.Context, string) (Role, error) { return Role{}, ErrNotFound }

func (s *roleByIDStub) GetByID(_ context.Context, id uuid.UUID) (Role, error) {
	if r, ok := s.byID[id]; ok {
		return r, nil
	}
	return Role{}, ErrNotFound
}

func (s *roleByIDStub) AllRolePermissions(context.Context) (map[string][]Permission, error) {
	return s.perms, nil
}
