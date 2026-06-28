package templ

import "github.com/a-h/templ"

// Component is a re-export of templ.Component so handlers in the web package can
// pass rendered components around without importing a-h/templ directly.
type Component = templ.Component
