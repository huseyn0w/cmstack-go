package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

// fakeUsersAdmin is a controllable UsersAdminService.
type fakeUsersAdmin struct {
	list      []accounts.User
	listTotal int
	roles     []accounts.Role
	get       accounts.User
	getErr    error
	updateErr error
	updated   accounts.User

	updateCalls []updateUserCall
}

type updateUserCall struct {
	id     uuid.UUID
	name   string
	roleID uuid.UUID
}

func (f *fakeUsersAdmin) ListUsers(context.Context, int, int) ([]accounts.User, int, error) {
	return f.list, f.listTotal, nil
}

func (f *fakeUsersAdmin) ListRoles(context.Context) ([]accounts.Role, error) {
	return f.roles, nil
}

func (f *fakeUsersAdmin) GetUser(context.Context, uuid.UUID) (accounts.User, error) {
	return f.get, f.getErr
}

func (f *fakeUsersAdmin) UpdateUser(_ context.Context, id uuid.UUID, name string, roleID uuid.UUID) (accounts.User, error) {
	f.updateCalls = append(f.updateCalls, updateUserCall{id: id, name: name, roleID: roleID})
	return f.updated, f.updateErr
}

func usersShell() adminShellDeps {
	return adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
}

func buildUsersAdminEnv(t *testing.T, svc UsersAdminService, authz PermissionChecker) (http.Handler, *scs.SessionManager, *AuthMiddleware, accounts.User) {
	t.Helper()
	user := accounts.User{ID: uuid.New(), Email: "ed@example.com", Name: "Ed"}
	sess := session.NewManager(false)
	users := fakeUsers{users: map[uuid.UUID]accounts.User{user.ID: user}}
	mw := NewAuthMiddleware(sess, users, authz)
	authH := accounts.NewHandler(stubAuthService{}, mw, security.Token, accounts.NewValidator())
	r := Router(Deps{
		Config:       config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:       health.NewHandler(health.NewService(nil)),
		Session:      sess,
		Auth:         authH,
		AuthMW:       mw,
		CSRFFunc:     security.Token,
		Authz:        authz,
		Roles:        fakeRoles{role: accounts.Role{Key: "administrator", Label: "Administrator"}},
		UserAdminSvc: svc,
	})
	return r, sess, mw, user
}

func TestUsersAdmin_UnauthenticatedRedirects(t *testing.T) {
	r, _, _, _ := buildUsersAdminEnv(t, &fakeUsersAdmin{}, allowAllAuthz{})
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauth /admin/users = %d, want 303", rec.Code)
	}
}

// TestUsersAdmin_DeniedPermissionIs403 mirrors the established
// denied-permission pattern used by every other admin area's tests (e.g.
// TestMediaAdmin_DeniedPermissionIs403): a GET, since CSRF is orthogonal to the
// permission gate and the router harness has no CSRF-token-minting helper for
// full-router POSTs. The POST route (/admin/users/{id}) is wired with the exact
// same RequirePermission(ActionUpdate, SubjectUser) gate shape as every other
// admin mutate route (see mountUsersAdmin), so this GET assertion exercises the
// same middleware wiring the POST route shares.
func TestUsersAdmin_DeniedPermissionIs403(t *testing.T) {
	r, sess, mw, user := buildUsersAdminEnv(t, &fakeUsersAdmin{}, denyAuthz{})
	cookie := mintSession(t, sess, mw, user)

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied GET /admin/users = %d, want 403", rec.Code)
	}
}

func TestUsersAdmin_ListRendersSeededUsersWithRoleLabels(t *testing.T) {
	adminRoleID, editorRoleID := uuid.New(), uuid.New()
	u1 := accounts.User{ID: uuid.New(), Name: "Alice Admin", Email: "alice@example.com", Username: "alice", RoleID: adminRoleID}
	u2 := accounts.User{ID: uuid.New(), Name: "Eddie Editor", Email: "eddie@example.com", Username: "eddie", RoleID: editorRoleID}
	svc := &fakeUsersAdmin{
		list:      []accounts.User{u1, u2},
		listTotal: 2,
		roles: []accounts.Role{
			{ID: adminRoleID, Key: "administrator", Label: "Administrator"},
			{ID: editorRoleID, Key: "editor", Label: "Editor"},
		},
	}
	h := NewUsersAdminHandler(svc, usersShell(), security.Token)

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("List = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Alice Admin", "alice@example.com", "Administrator",
		"Eddie Editor", "eddie@example.com", "Editor",
		"/admin/users/" + u1.ID.String() + "/edit",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list body missing %q", want)
		}
	}
}

func TestUsersAdmin_EditGETPrefillsNameAndSelectsCurrentRole(t *testing.T) {
	adminRoleID, editorRoleID := uuid.New(), uuid.New()
	id := uuid.New()
	svc := &fakeUsersAdmin{
		get: accounts.User{ID: id, Name: "Alice Admin", Email: "alice@example.com", Username: "alice", RoleID: editorRoleID},
		roles: []accounts.Role{
			{ID: adminRoleID, Key: "administrator", Label: "Administrator"},
			{ID: editorRoleID, Key: "editor", Label: "Editor"},
		},
	}
	h := NewUsersAdminHandler(svc, usersShell(), security.Token)

	req := httptest.NewRequest(http.MethodGet, "/admin/users/"+id.String()+"/edit", nil)
	req = withChiParam(req, "id", id.String())
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Edit = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="Alice Admin"`) {
		t.Error("name not pre-filled")
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Error("email not shown")
	}
	// The current role (Editor) must be the selected option; Administrator must
	// be present but NOT selected.
	if !strings.Contains(body, `value="`+editorRoleID.String()+`" selected`) {
		t.Errorf("editor role not marked selected:\n%s", body)
	}
	if strings.Contains(body, `value="`+adminRoleID.String()+`" selected`) {
		t.Error("administrator role must not be selected")
	}
}

func TestUsersAdmin_EditNotFoundOnUnknownUser(t *testing.T) {
	svc := &fakeUsersAdmin{getErr: accounts.ErrNotFound}
	h := NewUsersAdminHandler(svc, usersShell(), security.Token)

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/users/"+id.String()+"/edit", nil)
	req = withChiParam(req, "id", id.String())
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Edit(unknown) = %d, want 404", rec.Code)
	}
}

func TestUsersAdmin_EditBadUUIDIs404(t *testing.T) {
	h := NewUsersAdminHandler(&fakeUsersAdmin{}, usersShell(), security.Token)

	req := httptest.NewRequest(http.MethodGet, "/admin/users/not-a-uuid/edit", nil)
	req = withChiParam(req, "id", "not-a-uuid")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Edit(bad uuid) = %d, want 404", rec.Code)
	}
}

func TestUsersAdmin_UpdateForwardsAndRedirectsOnSuccess(t *testing.T) {
	id := uuid.New()
	roleID := uuid.New()
	svc := &fakeUsersAdmin{get: accounts.User{ID: id, RoleID: roleID}}
	h := NewUsersAdminHandler(svc, usersShell(), security.Token)

	form := url.Values{"name": {"  New Name  "}, "roleId": {roleID.String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiParam(req, "id", id.String())
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Update = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	want := "/admin/users/" + id.String() + "/edit?saved=1"
	if loc := rec.Header().Get("Location"); loc != want {
		t.Errorf("redirect = %q, want %q", loc, want)
	}
	if len(svc.updateCalls) != 1 {
		t.Fatalf("expected 1 UpdateUser call, got %d", len(svc.updateCalls))
	}
	call := svc.updateCalls[0]
	if call.id != id || call.name != "New Name" || call.roleID != roleID {
		t.Errorf("UpdateUser called with %+v, want id=%s name=%q roleID=%s", call, id, "New Name", roleID)
	}
}

func TestUsersAdmin_UpdateInvalidNameRerendersNoServiceCall(t *testing.T) {
	id := uuid.New()
	roleID := uuid.New()
	svc := &fakeUsersAdmin{
		get:   accounts.User{ID: id, Name: "Old Name", RoleID: roleID},
		roles: []accounts.Role{{ID: roleID, Key: "editor", Label: "Editor"}},
	}
	h := NewUsersAdminHandler(svc, usersShell(), security.Token)

	form := url.Values{"name": {"   "}, "roleId": {roleID.String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiParam(req, "id", id.String())
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Update(blank name) = %d, want 400\n%s", rec.Code, rec.Body.String())
	}
	if len(svc.updateCalls) != 0 {
		t.Error("UpdateUser must not be called when name is blank")
	}
	if !strings.Contains(rec.Body.String(), "Name is required") {
		t.Error("expected a name-required field error")
	}
}

func TestUsersAdmin_UpdateInvalidRoleIDRerendersNoServiceCall(t *testing.T) {
	id := uuid.New()
	svc := &fakeUsersAdmin{get: accounts.User{ID: id, Name: "Old Name"}}
	h := NewUsersAdminHandler(svc, usersShell(), security.Token)

	form := url.Values{"name": {"Ok Name"}, "roleId": {"not-a-uuid"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiParam(req, "id", id.String())
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Update(bad roleId) = %d, want 400", rec.Code)
	}
	if len(svc.updateCalls) != 0 {
		t.Error("UpdateUser must not be called when roleId does not parse")
	}
}

func TestUsersAdmin_UpdateErrLastAdminShowsFormErrorNoRedirect(t *testing.T) {
	id := uuid.New()
	roleID := uuid.New()
	svc := &fakeUsersAdmin{
		get:       accounts.User{ID: id, Name: "Old Name", RoleID: roleID},
		roles:     []accounts.Role{{ID: roleID, Key: "member", Label: "Member"}},
		updateErr: accounts.ErrLastAdmin,
	}
	h := NewUsersAdminHandler(svc, usersShell(), security.Token)

	form := url.Values{"name": {"New Name"}, "roleId": {roleID.String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiParam(req, "id", id.String())
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code == http.StatusSeeOther {
		t.Fatalf("Update(ErrLastAdmin) must not redirect, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "last administrator") {
		t.Errorf("expected a last-administrator error message:\n%s", rec.Body.String())
	}
}

func TestUsersAdmin_UpdateErrRoleNotFoundShowsFieldError(t *testing.T) {
	id := uuid.New()
	roleID := uuid.New()
	svc := &fakeUsersAdmin{
		get:       accounts.User{ID: id, Name: "Old Name", RoleID: roleID},
		roles:     []accounts.Role{{ID: roleID, Key: "editor", Label: "Editor"}},
		updateErr: accounts.ErrRoleNotFound,
	}
	h := NewUsersAdminHandler(svc, usersShell(), security.Token)

	form := url.Values{"name": {"New Name"}, "roleId": {uuid.New().String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiParam(req, "id", id.String())
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code == http.StatusSeeOther {
		t.Fatalf("Update(ErrRoleNotFound) must not redirect, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="error-roleId"`) {
		t.Errorf("expected a roleId field error:\n%s", rec.Body.String())
	}
}

func TestUsersAdmin_UpdateBadUUIDIs404(t *testing.T) {
	h := NewUsersAdminHandler(&fakeUsersAdmin{}, usersShell(), security.Token)

	form := url.Values{"name": {"X"}, "roleId": {uuid.New().String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/not-a-uuid", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiParam(req, "id", "not-a-uuid")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Update(bad uuid) = %d, want 404", rec.Code)
	}
}

// no-leakage: the rendered list/edit output must never include a password hash.
func TestUsersAdmin_NeverLeaksPasswordHash(t *testing.T) {
	id := uuid.New()
	roleID := uuid.New()
	svc := &fakeUsersAdmin{
		list:      []accounts.User{{ID: id, Name: "Alice", Email: "alice@example.com", RoleID: roleID, PasswordHash: "supersecrethash"}},
		listTotal: 1,
		get:       accounts.User{ID: id, Name: "Alice", Email: "alice@example.com", RoleID: roleID, PasswordHash: "supersecrethash"},
		roles:     []accounts.Role{{ID: roleID, Key: "editor", Label: "Editor"}},
	}
	h := NewUsersAdminHandler(svc, usersShell(), security.Token)

	listReq := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	listReq = listReq.WithContext(withUser(listReq.Context(), accounts.User{ID: uuid.New()}))
	listRec := httptest.NewRecorder()
	h.List(listRec, listReq)

	editReq := httptest.NewRequest(http.MethodGet, "/admin/users/"+id.String()+"/edit", nil)
	editReq = withChiParam(editReq, "id", id.String())
	editReq = editReq.WithContext(withUser(editReq.Context(), accounts.User{ID: uuid.New()}))
	editRec := httptest.NewRecorder()
	h.Edit(editRec, editReq)

	if strings.Contains(listRec.Body.String(), "supersecrethash") {
		t.Error("list view leaks the password hash")
	}
	if strings.Contains(editRec.Body.String(), "supersecrethash") {
		t.Error("edit view leaks the password hash")
	}
}
