package templ

// SettingsGeneralView is the view-model for the General settings dashboard. Each
// field carries the current effective value (override || config default) for
// pre-filling; Error renders an inline banner; Saved renders a success banner.
type SettingsGeneralView struct {
	Shell     AdminShell
	SaveURL   string
	CSRFToken string

	SiteName        string
	SiteDescription string
	DefaultOGImage  string
	TwitterHandle   string
	ContactEmail    string

	Error string
	Saved bool
}
