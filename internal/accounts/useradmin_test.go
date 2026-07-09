package accounts

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// fakeUserAdminUsers records the write and serves canned reads.
type fakeUserAdminUsers struct {
	list        []User
	total       int
	byID        map[uuid.UUID]User
	byRoleCount map[uuid.UUID]int
	updatedID   uuid.UUID
	updName     string
	updRole     uuid.UUID
	updated     bool
}

func (f *fakeUserAdminUsers) List(_ context.Context, _, _ int) ([]User, error) {
	return f.list, nil
}

func (f *fakeUserAdminUsers) Count(_ context.Context) (int, error) { return f.total, nil }

func (f *fakeUserAdminUsers) CountByRole(_ context.Context, roleID uuid.UUID) (int, error) {
	return f.byRoleCount[roleID], nil
}

func (f *fakeUserAdminUsers) GetByID(_ context.Context, id uuid.UUID) (User, error) {
	if u, ok := f.byID[id]; ok {
		return u, nil
	}
	return User{}, ErrNotFound
}

func (f *fakeUserAdminUsers) UpdateAdmin(_ context.Context, id uuid.UUID, name string, roleID uuid.UUID) (User, error) {
	f.updated = true
	f.updatedID = id
	f.updName = name
	f.updRole = roleID
	return User{ID: id, Name: name, RoleID: roleID}, nil
}

// fakeUserAdminRoles serves roles by id from a set.
type fakeUserAdminRoles struct{ byID map[uuid.UUID]Role }

func (f fakeUserAdminRoles) List(_ context.Context) ([]Role, error) {
	out := make([]Role, 0, len(f.byID))
	for _, r := range f.byID {
		out = append(out, r)
	}
	return out, nil
}

func (f fakeUserAdminRoles) GetByID(_ context.Context, id uuid.UUID) (Role, error) {
	if r, ok := f.byID[id]; ok {
		return r, nil
	}
	return Role{}, ErrNotFound
}

func TestUserAdminUpdateUserValidatesRole(t *testing.T) {
	userID := uuid.New()
	roleID := uuid.New()
	// Target currently holds the (non-admin) editor role, so the last-admin
	// guard is a no-op and the write proceeds.
	users := &fakeUserAdminUsers{byID: map[uuid.UUID]User{userID: {ID: userID, RoleID: roleID}}}
	roles := fakeUserAdminRoles{byID: map[uuid.UUID]Role{roleID: {ID: roleID, Key: RoleEditor, Label: "Editor"}}}
	svc := NewUserAdminService(users, roles)

	// Known role -> write happens with the supplied fields.
	got, err := svc.UpdateUser(context.Background(), userID, "New Name", roleID)
	if err != nil {
		t.Fatalf("UpdateUser: unexpected error %v", err)
	}
	if !users.updated || users.updatedID != userID || users.updName != "New Name" || users.updRole != roleID {
		t.Errorf("write not recorded correctly: %+v", users)
	}
	if got.Name != "New Name" || got.RoleID != roleID {
		t.Errorf("returned user wrong: %+v", got)
	}
}

func TestUserAdminUpdateUserUnknownRole(t *testing.T) {
	userID := uuid.New()
	users := &fakeUserAdminUsers{}
	roles := fakeUserAdminRoles{byID: map[uuid.UUID]Role{}}
	svc := NewUserAdminService(users, roles)

	_, err := svc.UpdateUser(context.Background(), userID, "X", uuid.New())
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("err = %v, want ErrRoleNotFound", err)
	}
	if users.updated {
		t.Error("write must not happen when role is unknown")
	}
}

func TestUserAdminUpdateUserLastAdminGuard(t *testing.T) {
	userID := uuid.New()
	adminRoleID := uuid.New()
	editorRoleID := uuid.New()
	roles := fakeUserAdminRoles{byID: map[uuid.UUID]Role{
		adminRoleID:  {ID: adminRoleID, Key: RoleAdministrator, Label: "Administrator"},
		editorRoleID: {ID: editorRoleID, Key: RoleEditor, Label: "Editor"},
	}}

	// Demoting the only administrator is refused and no write happens.
	soleAdmin := &fakeUserAdminUsers{
		byID:        map[uuid.UUID]User{userID: {ID: userID, RoleID: adminRoleID}},
		byRoleCount: map[uuid.UUID]int{adminRoleID: 1},
	}
	svc := NewUserAdminService(soleAdmin, roles)
	if _, err := svc.UpdateUser(context.Background(), userID, "X", editorRoleID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("err = %v, want ErrLastAdmin", err)
	}
	if soleAdmin.updated {
		t.Error("write must not happen when demoting the last administrator")
	}

	// With a second administrator present, the demotion proceeds.
	twoAdmins := &fakeUserAdminUsers{
		byID:        map[uuid.UUID]User{userID: {ID: userID, RoleID: adminRoleID}},
		byRoleCount: map[uuid.UUID]int{adminRoleID: 2},
	}
	svc = NewUserAdminService(twoAdmins, roles)
	if _, err := svc.UpdateUser(context.Background(), userID, "X", editorRoleID); err != nil {
		t.Fatalf("UpdateUser: unexpected error %v", err)
	}
	if !twoAdmins.updated || twoAdmins.updRole != editorRoleID {
		t.Errorf("demotion should proceed with another admin present: %+v", twoAdmins)
	}
}
