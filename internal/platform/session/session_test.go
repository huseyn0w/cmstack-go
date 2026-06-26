package session

import "testing"

func TestNewManager(t *testing.T) {
	m := NewManager(true)
	if m.Store == nil {
		t.Fatal("store not set")
	}
	if !m.Cookie.Secure {
		t.Error("expected secure cookie in production")
	}
	if !m.Cookie.HttpOnly {
		t.Error("expected HttpOnly cookie")
	}
	if Middleware(m) == nil {
		t.Error("middleware should not be nil")
	}
}
