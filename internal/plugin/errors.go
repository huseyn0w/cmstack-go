package plugin

import "errors"

// ErrUnknownPlugin is returned by SetEnabled when the id is not in the
// catalogue (registration is static, so an unknown id is always a caller bug).
var ErrUnknownPlugin = errors.New("plugin: unknown plugin id")

// ErrNoStore is returned by SetEnabled when the manager has no EnabledStore to
// persist to.
var ErrNoStore = errors.New("plugin: no enabled store configured")
