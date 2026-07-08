package templ

import "context"

// pluginSource is satisfied by the web package's plugin-manager accessor. It is
// an interface (rather than a direct import) so the templ package does not
// import the web/plugin packages, avoiding an import cycle. The web package
// registers its accessor via SetPluginSource when a manager is wired, mirroring
// SetThemeSource.
type pluginSource interface {
	RenderRegion(ctx context.Context, name string) []string
}

// pluginSrc is the registered accessor; nil until the web package wires it.
var pluginSrc pluginSource

// SetPluginSource registers the render-region accessor used by PluginRegion. The
// web package calls this from Router when a plugin manager is present, so the
// public layout can inject plugin fragments without importing web (which would
// cycle). When no manager is wired the accessor stays nil and PluginRegion
// yields nothing.
func SetPluginSource(s pluginSource) { pluginSrc = s }

// PluginRegion returns the trusted HTML fragments contributed by enabled plugins
// for the named render-region, or nil when no plugin source is registered. The
// layout emits each fragment via templ.Raw (plugins are first-party Go code, so
// their output is trusted).
func PluginRegion(ctx context.Context, name string) []string {
	if pluginSrc == nil {
		return nil
	}
	return pluginSrc.RenderRegion(ctx, name)
}
