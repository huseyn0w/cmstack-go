package templ

import "strconv"

// itoa renders an int as a decimal string for the menus templates.
func itoa(n int) string { return strconv.Itoa(n) }

// MenuRow is one menu in the admin menus list: its identity, assigned location
// (empty when unassigned), item count, and the editor link.
type MenuRow struct {
	ID        string
	Name      string
	Location  string // "" when unassigned
	ItemCount int
	EditURL   string
}

// LocationOption is a selectable menu location (header/footer/none) in the
// create/settings forms.
type LocationOption struct {
	Value    string // "", "header", "footer"
	Label    string
	Selected bool
}

// MenusListView is the admin menus index view-model: every menu plus a
// "new menu" form (name + a location select). Error surfaces a friendly inline
// message (e.g. a location collision) after a failed create.
type MenusListView struct {
	Shell     AdminShell
	Rows      []MenuRow
	CreateURL string // POST target for the new-menu form
	Locations []LocationOption
	NewName   string // sticky name after a failed create
	Error     string
	CSRFToken string
}

// MenuItemRow is one item row in the menu editor: its label, the resolved link
// text (URL), the reorder/edit/delete action targets, and whether it is first or
// last (so the up/down buttons can be disabled at the ends).
type MenuItemRow struct {
	ID        string
	Label     string
	URL       string
	Type      string
	EditURL   string
	MoveURL   string // POST target for up/down (dir field selects direction)
	DeleteURL string // POST target to remove the item
	IsFirst   bool
	IsLast    bool
}

// MenuContentChoice is one selectable piece of content (post/page/category) in
// the add-item picker: value is the content id, Label its title/name.
type MenuContentChoice struct {
	Value string
	Label string
}

// MenuEditorView is the menu editor view-model: the settings form (name +
// location), the ordered item list, and the add-item form (a type select that
// reveals the right input via Alpine, plus three content selects and the custom
// URL/label inputs).
type MenuEditorView struct {
	Shell      AdminShell
	ID         string
	Name       string
	Location   string
	Locations  []LocationOption
	Items      []MenuItemRow
	Posts      []MenuContentChoice
	Pages      []MenuContentChoice
	Categories []MenuContentChoice

	SettingsURL string // POST target for rename + assign location
	DeleteURL   string // POST target to delete the whole menu
	AddItemURL  string // POST target for the add-item form

	Error     string
	CSRFToken string
}

// MenuItemLabelInput is one non-default-locale label input in the item editor.
type MenuItemLabelInput struct {
	Locale string // de / ru
	Label  string // display label for the field (e.g. "German")
	Value  string // pre-filled value (empty when no read is available)
}

// MenuItemEditView is the per-item editor view-model: the base (en) label, one
// label input per non-default locale, and — for custom items — the editable URL.
type MenuItemEditView struct {
	Shell        AdminShell
	MenuID       string
	ItemID       string
	Label        string
	URL          string
	IsCustom     bool
	LocaleInputs []MenuItemLabelInput
	ActionURL    string // POST target
	BackURL      string
	Error        string
	CSRFToken    string
}
