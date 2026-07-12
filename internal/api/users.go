package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
)

// listUsers serves GET /api/v1/users: a paginated user listing (admin-only DTO,
// no sensitive fields). Role labels are resolved via a one-shot role lookup so
// each row carries its roleName without an N+1.
func (h *handler) listUsers(w http.ResponseWriter, r *http.Request) {
	page, perPage := paginate(r)
	items, total, err := h.users.ListUsers(r.Context(), perPage, (page-1)*perPage)
	if err != nil {
		Fail(w, http.StatusInternalServerError, "internal", "failed to list users")
		return
	}
	roleName := h.roleNamer(r)
	dtos := make([]userDTO, 0, len(items))
	for _, u := range items {
		dtos = append(dtos, toUserDTO(u, roleName))
	}
	OK(w, http.StatusOK, listResponse{Items: dtos, Total: total, Page: page, PerPage: perPage})
}

// listRoles serves GET /api/v1/roles: every role (the admin role picker).
func (h *handler) listRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.users.ListRoles(r.Context())
	if err != nil {
		Fail(w, http.StatusInternalServerError, "internal", "failed to list roles")
		return
	}
	dtos := make([]roleDTO, 0, len(roles))
	for _, role := range roles {
		dtos = append(dtos, toRoleDTO(role))
	}
	OK(w, http.StatusOK, dtos)
}

// getUser serves GET /api/v1/users/{id}: a single user (admin DTO). 404 when
// absent.
func (h *handler) getUser(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "user")
	if !ok {
		return
	}
	u, err := h.users.GetUser(r.Context(), id)
	if err != nil {
		writeUserError(w, err)
		return
	}
	OK(w, http.StatusOK, toUserDTO(u, h.roleNamer(r)))
}

// updateUserRequest is the partial JSON body for PATCH /api/v1/users/{id}. Both
// fields are optional; only provided fields change (name/role).
type updateUserRequest struct {
	Name   *string `json:"name"`
	RoleID *string `json:"roleId"`
}

// updateUser serves PATCH /api/v1/users/{id}: applies the provided name/role.
// It loads the current user, overlays the provided fields, then persists via the
// service (which validates the role exists).
func (h *handler) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "user")
	if !ok {
		return
	}
	var req updateUserRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}

	current, err := h.users.GetUser(r.Context(), id)
	if err != nil {
		writeUserError(w, err)
		return
	}

	name := current.Name
	if req.Name != nil {
		name = *req.Name
	}
	roleID := current.RoleID
	if req.RoleID != nil {
		parsed, perr := uuid.Parse(*req.RoleID)
		if perr != nil {
			FailValidation(w, map[string]string{"roleId": "must be a valid uuid"})
			return
		}
		roleID = parsed
	}

	updated, err := h.users.UpdateUser(r.Context(), id, name, roleID)
	if err != nil {
		writeUserError(w, err)
		return
	}
	OK(w, http.StatusOK, toUserDTO(updated, h.roleNamer(r)))
}

// roleNamer returns a role-id -> label resolver backed by a single ListRoles
// call. It degrades to an empty label on a lookup failure so a role read error
// never fails the user read (roleName is best-effort metadata).
func (h *handler) roleNamer(r *http.Request) func(id uuid.UUID) string {
	roles, err := h.users.ListRoles(r.Context())
	if err != nil {
		return func(uuid.UUID) string { return "" }
	}
	byID := make(map[uuid.UUID]string, len(roles))
	for _, role := range roles {
		byID[role.ID] = role.Label
	}
	return func(id uuid.UUID) string { return byID[id] }
}

// writeUserError maps a users-admin error onto the uniform JSON envelope:
// not-found -> 404, unknown-role -> 422, last-admin demotion -> 409, everything
// else -> 500.
func writeUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, accounts.ErrNotFound):
		Fail(w, http.StatusNotFound, "not_found", "user not found")
	case errors.Is(err, accounts.ErrRoleNotFound):
		FailValidation(w, map[string]string{"roleId": "unknown role"})
	case errors.Is(err, accounts.ErrLastAdmin):
		Fail(w, http.StatusConflict, "last_admin", "cannot demote the last administrator")
	default:
		Fail(w, http.StatusInternalServerError, "internal", "failed to process user")
	}
}
