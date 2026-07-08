package templ

// PluginRow is one plugin in the admin plugin manager: its registry identity
// plus whether it is currently enabled.
type PluginRow struct {
	ID          string
	Name        string
	Description string
	Enabled     bool
}

// PluginsView is the admin plugin-manager page view-model: the full plugin
// catalogue with a per-row enable/disable toggle.
type PluginsView struct {
	Shell     AdminShell
	Plugins   []PluginRow
	ToggleURL string // POST target; submits the plugin id + desired state
	CSRFToken string
}
