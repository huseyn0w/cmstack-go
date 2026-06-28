package accounts

import "context"

// StaticSettings is a config-backed SettingsProvider. The values are resolved
// once at startup from config; the admin-UI-backed provider (which reads the
// settings table and can change at runtime) replaces this in M15.
type StaticSettings struct {
	signupEnabled             bool
	emailVerificationRequired bool
}

// NewStaticSettings constructs a StaticSettings from resolved config toggles.
func NewStaticSettings(signupEnabled, emailVerificationRequired bool) StaticSettings {
	return StaticSettings{
		signupEnabled:             signupEnabled,
		emailVerificationRequired: emailVerificationRequired,
	}
}

// SignupEnabled implements SettingsProvider.
func (s StaticSettings) SignupEnabled(context.Context) bool { return s.signupEnabled }

// EmailVerificationRequired implements SettingsProvider.
func (s StaticSettings) EmailVerificationRequired(context.Context) bool {
	return s.emailVerificationRequired
}

var _ SettingsProvider = StaticSettings{}
