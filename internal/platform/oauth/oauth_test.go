package oauth

import (
	"testing"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
)

func providerNames(ps []Provider) map[string]bool {
	out := map[string]bool{}
	for _, p := range ps {
		out[p.Name] = true
	}
	return out
}

func TestSetupOnlyEnablesConfiguredProviders(t *testing.T) {
	t.Run("no credentials -> no providers", func(t *testing.T) {
		goth.ClearProviders()
		enabled := Setup(Config{CallbackBase: "https://x.test"})
		if len(enabled) != 0 {
			t.Fatalf("expected 0 providers, got %d", len(enabled))
		}
		if len(goth.GetProviders()) != 0 {
			t.Error("goth should have no registered providers")
		}
	})

	t.Run("only google configured", func(t *testing.T) {
		goth.ClearProviders()
		enabled := Setup(Config{
			CallbackBase:       "https://x.test",
			GoogleClientID:     "gid",
			GoogleClientSecret: "gsecret",
		})
		names := providerNames(enabled)
		if !names[ProviderGoogle] || names[ProviderGitHub] {
			t.Errorf("expected only google enabled, got %v", names)
		}
		if _, err := goth.GetProvider(ProviderGoogle); err != nil {
			t.Errorf("google should be registered with goth: %v", err)
		}
	})

	t.Run("both configured", func(t *testing.T) {
		goth.ClearProviders()
		enabled := Setup(Config{
			CallbackBase:       "https://x.test",
			GoogleClientID:     "gid",
			GoogleClientSecret: "gsecret",
			GitHubClientID:     "hid",
			GitHubClientSecret: "hsecret",
		})
		names := providerNames(enabled)
		if !names[ProviderGoogle] || !names[ProviderGitHub] {
			t.Errorf("expected both providers, got %v", names)
		}
	})

	t.Run("partial credentials do not enable a provider", func(t *testing.T) {
		goth.ClearProviders()
		enabled := Setup(Config{
			CallbackBase:   "https://x.test",
			GoogleClientID: "gid", // secret missing
		})
		if len(enabled) != 0 {
			t.Fatalf("client id without secret must not enable google, got %v", enabled)
		}
	})
}

func TestSetupInstallsSessionStore(t *testing.T) {
	goth.ClearProviders()
	_ = Setup(Config{SessionKey: "some-key"})
	// gothic.Store is set by Setup; a nil store would panic on first use.
	if gothic.Store == nil {
		t.Error("gothic store should be installed by Setup")
	}
}
