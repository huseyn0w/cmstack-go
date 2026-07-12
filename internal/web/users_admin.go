package web

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// UsersAdminService is the narrow user-admin dependency the web handler needs:
// paginated listing, the role catalogue (for the picker + label resolution), a
// single user read, and the admin-editable-fields update (name, role).
// *accounts.UserAdminService satisfies it. Declaring it here keeps web
// decoupled from constructing the service and trivially fakeable in tests.
type UsersAdminService interface {
	ListUsers(ctx context.Context, limit, offset int) ([]accounts.User, int, error)
	ListRoles(ctx context.Context) ([]accounts.Role, error)
	GetUser(ctx context.Context, id uuid.UUID) (accounts.User, error)
	UpdateUser(ctx context.Context, id uuid.UUID, name string, roleID uuid.UUID) (accounts.User, error)
}

// UsersAdminHandler is the thin HTTP boundary for the admin users area: it
// lists users with their resolved role label and renders/handles the per-user
// name + role edit form. It holds no business logic (role validation and the
// last-administrator guard live in UserAdminService).
type UsersAdminHandler struct {
	svc   UsersAdminService
	shell adminShellDeps
	csrf  func(*http.Request) string
}

// NewUsersAdminHandler constructs the admin users handler.
func NewUsersAdminHandler(svc UsersAdminService, shell adminShellDeps, csrf func(*http.Request) string) *UsersAdminHandler {
	return &UsersAdminHandler{svc: svc, shell: shell, csrf: csrf}
}

// List renders the paginated admin users table, resolving each row's role
// label from a single ListRoles call.
func (h *UsersAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	items, total, err := h.svc.ListUsers(r.Context(), adminPageSize, (page-1)*adminPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	roles, err := h.svc.ListRoles(r.Context())
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	labelByRole := make(map[uuid.UUID]string, len(roles))
	for _, ro := range roles {
		labelByRole[ro.ID] = ro.Label
	}

	rows := make([]webtempl.UserRow, 0, len(items))
	for _, u := range items {
		rows = append(rows, webtempl.UserRow{
			ID:        u.ID.String(),
			Name:      u.Name,
			Email:     u.Email,
			Username:  u.Username,
			RoleLabel: labelByRole[u.RoleID],
			EditURL:   "/admin/users/" + u.ID.String() + "/edit",
		})
	}
	view := webtempl.UsersListView{
		Shell: h.shell.buildShell(r, "Users"),
		Rows:  rows,
		Pager: pager(page, adminPageSize, total, "/admin/users", ""),
	}
	h.render(w, r, http.StatusOK, webtempl.UsersListPage(view))
}

// Edit renders the edit form for the user identified by the {id} route param:
// name pre-filled, and a role select populated from ListRoles with the user's
// current role marked selected. 404s on a malformed id or accounts.ErrNotFound.
func (h *UsersAdminHandler) Edit(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	u, err := h.svc.GetUser(r.Context(), id)
	if errors.Is(err, accounts.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	roles, err := h.svc.ListRoles(r.Context())
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	view := webtempl.UserEditView{
		Shell:       h.shell.buildShell(r, "Edit user"),
		ID:          u.ID.String(),
		Name:        u.Name,
		Email:       u.Email,
		Username:    u.Username,
		Roles:       roleOptions(roles, u.RoleID.String()),
		ActionURL:   "/admin/users/" + u.ID.String(),
		BackURL:     "/admin/users",
		CSRFToken:   h.csrf(r),
		FieldErrors: map[string]string{},
		Saved:       r.URL.Query().Get("saved") == "1",
	}
	h.render(w, r, http.StatusOK, webtempl.UserEditPage(view))
}

// Update handles the edit form POST: it validates the submitted name and role
// id BEFORE calling the service (so an invalid submission never reaches it),
// then maps the service's domain errors to the appropriate field/form error.
// On success it redirects to the edit form with a success banner.
func (h *UsersAdminHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	name := strings.TrimSpace(r.PostFormValue("name"))
	roleIDRaw := r.PostFormValue("roleId")

	fieldErrs := map[string]string{}
	if name == "" {
		fieldErrs["name"] = "Name is required."
	}
	roleID, roleErr := uuid.Parse(roleIDRaw)
	if roleErr != nil {
		fieldErrs["roleId"] = "Choose a valid role."
	}
	if len(fieldErrs) > 0 {
		h.renderEditError(w, r, id, name, roleIDRaw, fieldErrs, "")
		return
	}

	_, err = h.svc.UpdateUser(r.Context(), id, name, roleID)
	switch {
	case errors.Is(err, accounts.ErrRoleNotFound):
		h.renderEditError(w, r, id, name, roleIDRaw, map[string]string{"roleId": "That role no longer exists."}, "")
		return
	case errors.Is(err, accounts.ErrLastAdmin):
		h.renderEditError(w, r, id, name, roleIDRaw, map[string]string{}, "Cannot demote the last administrator.")
		return
	case errors.Is(err, accounts.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/users/"+id.String()+"/edit?saved=1", http.StatusSeeOther)
}

// renderEditError re-loads the user + role catalogue to re-render the edit
// form with the submitted (rejected) values and the given field/form errors.
func (h *UsersAdminHandler) renderEditError(w http.ResponseWriter, r *http.Request, id uuid.UUID, name, roleIDRaw string, fieldErrs map[string]string, formErr string) {
	u, err := h.svc.GetUser(r.Context(), id)
	if errors.Is(err, accounts.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	roles, err := h.svc.ListRoles(r.Context())
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	view := webtempl.UserEditView{
		Shell:       h.shell.buildShell(r, "Edit user"),
		ID:          id.String(),
		Name:        name,
		Email:       u.Email,
		Username:    u.Username,
		Roles:       roleOptions(roles, roleIDRaw),
		ActionURL:   "/admin/users/" + id.String(),
		BackURL:     "/admin/users",
		CSRFToken:   h.csrf(r),
		FieldErrors: fieldErrs,
		Error:       formErr,
	}
	h.render(w, r, http.StatusBadRequest, webtempl.UserEditPage(view))
}

// roleOptions builds the role-select options, marking selectedID's match.
func roleOptions(roles []accounts.Role, selectedID string) []webtempl.RoleOption {
	out := make([]webtempl.RoleOption, 0, len(roles))
	for _, ro := range roles {
		out = append(out, webtempl.RoleOption{
			ID:       ro.ID.String(),
			Label:    ro.Label,
			Selected: ro.ID.String() == selectedID,
		})
	}
	return out
}

func (h *UsersAdminHandler) render(w http.ResponseWriter, r *http.Request, status int, c webtempl.Component) {
	if err := render.Component(r.Context(), w, status, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
