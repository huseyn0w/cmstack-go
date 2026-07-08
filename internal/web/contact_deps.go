package web

import (
	"context"
	"strings"
)

// contactSettingsReader is the subset of *settings.Service the recipient
// resolver reads. *settings.Service satisfies it.
type contactSettingsReader interface {
	Get(ctx context.Context, key string) (string, bool, error)
}

// contactRecipientSettingKey is the settings key holding the contact-form
// recipient email. When unset/empty the resolver falls back to the config
// default, then the admin email.
const contactRecipientSettingKey = "contact_recipient"

// ContactRecipientResolver satisfies contact.RecipientResolver. It resolves the
// contact-form recipient with the precedence: settings key `contact_recipient`
// → the ContactRecipient config default → the AdminEmail. A lookup error or an
// empty settings value falls through to the next source (it never fails the
// drain — an empty result makes the listener no-op).
type ContactRecipientResolver struct {
	settings    contactSettingsReader
	configValue string
	adminEmail  string
}

// NewContactRecipientResolver constructs the resolver. settings may be nil (the
// settings layer is not wired), in which case only configValue/adminEmail apply.
func NewContactRecipientResolver(settings contactSettingsReader, configValue, adminEmail string) *ContactRecipientResolver {
	return &ContactRecipientResolver{
		settings:    settings,
		configValue: strings.TrimSpace(configValue),
		adminEmail:  strings.TrimSpace(adminEmail),
	}
}

// Recipient resolves the recipient email (settings → config → admin), returning
// "" only when every source is empty.
func (r *ContactRecipientResolver) Recipient(ctx context.Context) string {
	if r.settings != nil {
		if v, ok, err := r.settings.Get(ctx, contactRecipientSettingKey); err == nil && ok {
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
		}
	}
	if r.configValue != "" {
		return r.configValue
	}
	return r.adminEmail
}

// contactMailer is the subset of *mailer.LogMailer the notifier adapter calls.
type contactMailer interface {
	SendContactNotification(ctx context.Context, to, fromEmail, fromName, subject, message string) error
}

// ContactNotifierAdapter bridges contact.Notifier onto the platform
// mailer's SendContactNotification. It exists for symmetry with the comments
// wiring (and to avoid the contact package importing the mailer package).
type ContactNotifierAdapter struct {
	mailer contactMailer
}

// NewContactNotifierAdapter wraps a mailer as a contact.Notifier.
func NewContactNotifierAdapter(m contactMailer) *ContactNotifierAdapter {
	return &ContactNotifierAdapter{mailer: m}
}

// SendContactNotification satisfies contact.Notifier.
func (a *ContactNotifierAdapter) SendContactNotification(ctx context.Context, to, fromEmail, fromName, subject, message string) error {
	return a.mailer.SendContactNotification(ctx, to, fromEmail, fromName, subject, message)
}
