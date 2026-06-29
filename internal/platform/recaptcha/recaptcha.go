// Package recaptcha provides an OPTIONAL reCAPTCHA v3 verifier. When no secret
// key is configured the verifier is disabled and Verify returns true, so the
// local/demo stack runs without Google keys (graceful no-op, mirroring the
// django/laravel/ts references). When a secret is configured, a token is
// required and must meet the score threshold.
package recaptcha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// verifyURL is Google's siteverify endpoint. It is a package var so tests can
// point the verifier at a fake server.
var verifyURL = "https://www.google.com/recaptcha/api/siteverify"

// siteVerifyResponse is the subset of Google's siteverify JSON we consume.
type siteVerifyResponse struct {
	Success bool    `json:"success"`
	Score   float64 `json:"score"`
}

// passes is the PURE decision: did a verification succeed and meet the score
// threshold? Directly unit-testable without any HTTP.
func passes(r siteVerifyResponse, minScore float64) bool {
	return r.Success && r.Score >= minScore
}

// Verifier verifies reCAPTCHA v3 tokens. The zero value is NOT usable; build it
// with New. A Verifier with an empty secret is "disabled" — Verify always
// returns (true, nil).
type Verifier struct {
	secret   string
	minScore float64
	client   *http.Client
}

// New constructs a Verifier. An empty secret disables verification (Verify is a
// no-op that allows). A non-positive minScore defaults to 0.5.
func New(secret string, minScore float64) *Verifier {
	if minScore <= 0 {
		minScore = 0.5
	}
	return &Verifier{
		secret:   strings.TrimSpace(secret),
		minScore: minScore,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled reports whether a secret is configured (verification is enforced).
func (v *Verifier) Enabled() bool { return v.secret != "" }

// Verify reports whether the token passes. When the verifier is disabled (no
// secret) it returns (true, nil). When enabled, an empty token is rejected
// (false, nil) and a configured token is checked against Google, requiring
// success + a score >= the threshold. A transport/decoding failure returns
// (false, err) so the caller can decide (the service treats it as a soft reject).
func (v *Verifier) Verify(ctx context.Context, token string) (bool, error) {
	if !v.Enabled() {
		return true, nil
	}
	if strings.TrimSpace(token) == "" {
		return false, nil
	}

	form := url.Values{}
	form.Set("secret", v.secret)
	form.Set("response", token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	var data siteVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return false, err
	}
	return passes(data, v.minScore), nil
}
