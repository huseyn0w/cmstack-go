// Package session wires the scs session manager. M0 uses an in-memory store;
// a Postgres-backed store is wired but optional and selected at startup.
package session

import (
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
)

// CookieName is the name of the session cookie set by NewManager. It is exported
// so out-of-band request inspection (e.g. the public page cache deciding whether
// a request may be authenticated) can detect the cookie by its exact name
// without importing scs.
const CookieName = "cmstack_session"

// NewManager builds an scs.SessionManager with the in-memory store. Cookies are
// HttpOnly and Lax; Secure is enabled in production.
func NewManager(production bool) *scs.SessionManager {
	m := scs.New()
	m.Store = memstore.New()
	m.Lifetime = 24 * time.Hour
	m.Cookie.Name = CookieName
	m.Cookie.HttpOnly = true
	m.Cookie.SameSite = http.SameSiteLaxMode
	m.Cookie.Secure = production
	m.Cookie.Path = "/"
	return m
}

// Middleware returns the scs LoadAndSave middleware for the manager.
func Middleware(m *scs.SessionManager) func(http.Handler) http.Handler {
	return m.LoadAndSave
}
