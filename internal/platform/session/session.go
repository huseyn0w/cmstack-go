// Package session wires the scs session manager. M0 uses an in-memory store;
// a Postgres-backed store is wired but optional and selected at startup.
package session

import (
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
)

// NewManager builds an scs.SessionManager with the in-memory store. Cookies are
// HttpOnly and Lax; Secure is enabled in production.
func NewManager(production bool) *scs.SessionManager {
	m := scs.New()
	m.Store = memstore.New()
	m.Lifetime = 24 * time.Hour
	m.Cookie.Name = "cmstack_session"
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
