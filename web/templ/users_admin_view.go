package templ

// UserRow is one row in the admin users list: the display fields plus a link to
// the per-user edit form. It carries NO sensitive fields (no password hash).
type UserRow struct {
	ID        string
	Name      string
	Email     string
	Username  string
	RoleLabel string
	EditURL   string
}

// UsersListView is the admin users list page view-model.
type UsersListView struct {
	Shell AdminShell
	Rows  []UserRow
	Pager Pagination
}

// RoleOption is one entry in the edit form's role picker.
type RoleOption struct {
	ID       string
	Label    string
	Selected bool
}

// UserEditView is the per-user admin edit form view-model (name + role). Email
// and username are shown read-only for identification; they are not editable
// here (the admin surface only edits the name and role, per UserAdminService).
type UserEditView struct {
	Shell       AdminShell
	ID          string
	Name        string
	Email       string
	Username    string
	Roles       []RoleOption
	ActionURL   string
	BackURL     string
	CSRFToken   string
	FieldErrors map[string]string
	Error       string
	Saved       bool
}
