package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
)

// fakeUserAdmin is an in-memory UserAdminService.
type fakeUserAdmin struct {
	list      []accounts.User
	total     int
	byID      map[uuid.UUID]accounts.User
	roles     []accounts.Role
	updatedID uuid.UUID
	updName   string
	updRole   uuid.UUID
	roleErr   error
}

func (f *fakeUserAdmin) ListUsers(context.Context, int, int) ([]accounts.User, int, error) {
	return f.list, f.total, nil
}

func (f *fakeUserAdmin) ListRoles(context.Context) ([]accounts.Role, error) {
	return f.roles, nil
}

func (f *fakeUserAdmin) GetUser(_ context.Context, id uuid.UUID) (accounts.User, error) {
	if u, ok := f.byID[id]; ok {
		return u, nil
	}
	return accounts.User{}, accounts.ErrNotFound
}

func (f *fakeUserAdmin) UpdateUser(_ context.Context, id uuid.UUID, name string, roleID uuid.UUID) (accounts.User, error) {
	if f.roleErr != nil {
		return accounts.User{}, f.roleErr
	}
	f.updatedID = id
	f.updName = name
	f.updRole = roleID
	u := f.byID[id]
	u.Name = name
	u.RoleID = roleID
	return u, nil
}

func TestListUsersNoSensitiveLeak(t *testing.T) {
	userID := uuid.New()
	roleID := uuid.New()
	fu := &fakeUserAdmin{
		total: 1,
		roles: []accounts.Role{{ID: roleID, Key: "editor", Label: "Editor"}},
		list: []accounts.User{{
			ID: uuid.New(), Email: "u@e.com", Username: "u", Name: "U", RoleID: roleID,
			PasswordHash: "SECRET-HASH", PasswordChangedAt: time.Now(), CreatedAt: time.Now(),
			SocialLinks: map[string]string{"twitter": "x"},
		}},
	}
	srv := newServerDeps(t, userID, map[string]bool{"read:user": true}, Deps{Users: fu})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/users"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decode(t, rec)["data"].(map[string]any)
	item := data["items"].([]any)[0].(map[string]any)
	if item["email"] != "u@e.com" || item["roleName"] != "Editor" {
		t.Errorf("user dto fields wrong: %v", item)
	}
	for _, bad := range []string{"passwordHash", "password_hash", "passwordChangedAt", "socialLinks"} {
		if _, leaked := item[bad]; leaked {
			t.Errorf("user DTO leaked %q", bad)
		}
	}
}

func TestListRolesEndpoint(t *testing.T) {
	userID := uuid.New()
	fu := &fakeUserAdmin{roles: []accounts.Role{{ID: uuid.New(), Key: "administrator", Label: "Administrator"}}}
	srv := newServerDeps(t, userID, map[string]bool{"read:user": true}, Deps{Users: fu})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/roles"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	roles := decode(t, rec)["data"].([]any)
	if len(roles) != 1 || roles[0].(map[string]any)["key"] != "administrator" {
		t.Errorf("roles dto wrong: %v", roles)
	}
}

func TestGetUserNotFound(t *testing.T) {
	userID := uuid.New()
	fu := &fakeUserAdmin{byID: map[uuid.UUID]accounts.User{}}
	srv := newServerDeps(t, userID, map[string]bool{"read:user": true}, Deps{Users: fu})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/users/"+uuid.New().String()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUpdateUserPartial(t *testing.T) {
	userID := uuid.New()
	target := uuid.New()
	oldRole := uuid.New()
	newRole := uuid.New()
	fu := &fakeUserAdmin{byID: map[uuid.UUID]accounts.User{
		target: {ID: target, Name: "Old", RoleID: oldRole},
	}}
	srv := newServerDeps(t, userID, map[string]bool{"update:user": true}, Deps{Users: fu})

	// Only change roleId; name must be preserved from the current record.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/users/"+target.String(), `{"roleId":"`+newRole.String()+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fu.updName != "Old" || fu.updRole != newRole {
		t.Errorf("partial apply wrong: name=%q role=%v", fu.updName, fu.updRole)
	}
}

func TestUpdateUserBadRoleID(t *testing.T) {
	userID := uuid.New()
	target := uuid.New()
	fu := &fakeUserAdmin{byID: map[uuid.UUID]accounts.User{target: {ID: target}}}
	srv := newServerDeps(t, userID, map[string]bool{"update:user": true}, Deps{Users: fu})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/users/"+target.String(), `{"roleId":"not-a-uuid"}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestUpdateUserUnknownRole422(t *testing.T) {
	userID := uuid.New()
	target := uuid.New()
	fu := &fakeUserAdmin{
		byID:    map[uuid.UUID]accounts.User{target: {ID: target}},
		roleErr: accounts.ErrRoleNotFound,
	}
	srv := newServerDeps(t, userID, map[string]bool{"update:user": true}, Deps{Users: fu})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/users/"+target.String(), `{"roleId":"`+uuid.New().String()+`"}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestUsersForbidden(t *testing.T) {
	userID := uuid.New()
	srv := newServerDeps(t, userID, map[string]bool{}, Deps{Users: &fakeUserAdmin{}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/users"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
