package templ

// TokenRow is one row in the self-service API token list: display-safe fields
// only (masked last-four, pre-formatted dates, a computed status). It NEVER
// carries the token hash or plaintext — the plaintext is shown exactly once,
// via TokensView.Revealed, immediately after Create, and is never persisted
// into a row that could reload.
type TokenRow struct {
	ID         string
	Name       string
	LastFour   string // trailing 4 chars of the plaintext; rendered as "…XXXX"
	CreatedAt  string
	LastUsedAt string // pre-formatted, or "Never"
	ExpiresAt  string // pre-formatted, or "Never"
	Status     string // "active" | "revoked" | "expired"
	RevokeURL  string
}

// TokensView is the /account/tokens page view-model.
type TokensView struct {
	Shell     AdminShell
	CSRFToken string
	Rows      []TokenRow

	// NameError is the create-form field error (e.g. a missing/too-long name).
	// No token is created when it is set.
	NameError string

	// Revealed is the ONE-TIME plaintext of a just-minted token; empty on every
	// render except the response immediately following a successful Create.
	Revealed     string
	RevealedName string

	// Revoked shows the "token revoked" banner after a redirect from Revoke.
	Revoked bool
}

func (v TokensView) hasTokens() bool { return len(v.Rows) > 0 }
