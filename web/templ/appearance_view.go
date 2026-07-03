package templ

// ThemeChoice is one selectable theme in the admin appearance switcher: its
// registry identity plus whether it is the currently active site theme.
type ThemeChoice struct {
	ID          string
	Label       string
	Description string
	Active      bool
}

// AppearanceView is the admin appearance (theme switcher) page view-model. It
// lists the registered themes with a live palette preview and a one-click
// activate action per non-active theme.
type AppearanceView struct {
	Shell       AdminShell
	Themes      []ThemeChoice
	ActivateURL string // POST target; the chosen theme id is submitted as "theme"
	CSRFToken   string
}
