package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// defaultTimeout bounds every REST call so a hung API never wedges a tool.
const defaultTimeout = 30 * time.Second

// APIError is the Go error a non-2xx REST response is mapped to. It carries the
// HTTP status plus the stable code and human message decoded from the API's
// {"error":{code,message}} envelope, so tools can surface a clean, actionable
// message (e.g. a 403 naming the RBAC boundary).
type APIError struct {
	// Status is the HTTP status code of the failed response.
	Status int
	// Code is the stable machine-readable token from the error envelope (e.g.
	// "forbidden", "not_found", "validation"). It may be empty if the body was
	// not a recognizable error envelope.
	Code string
	// Message is the human-readable explanation from the error envelope.
	Message string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("API error %d (%s): %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("API error %d: %s", e.Status, e.Message)
}

// APIClient is a thin, authenticated HTTP client of the CMStack-Go REST API. It
// sets the bearer token on every request; it carries NO authorization logic of
// its own — the API re-checks RBAC per call.
type APIClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// New constructs an APIClient for the given REST origin and bearer token. When
// httpClient is nil a std *http.Client with a sane timeout is used. baseURL
// must be the origin only (no /api/v1 suffix).
func New(baseURL, token string, httpClient *http.Client) *APIClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &APIClient{baseURL: baseURL, token: token, http: httpClient}
}

// do performs an authenticated request against /api/v1 + path. It sets the
// Bearer header and (for a non-nil body) Content-Type: application/json, encodes
// body as JSON, and on a 2xx unwraps the {"data":...} envelope, returning the
// raw JSON of the data field. A non-2xx response is decoded from the
// {"error":{code,message}} envelope into an *APIError. query may be nil.
//
// A 204 (or empty 2xx body) yields a nil result and nil error (a successful
// delete carries no data).
func (c *APIClient) do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	u := c.baseURL + "/api/v1" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeAPIError(resp.StatusCode, raw)
	}

	if resp.StatusCode == http.StatusNoContent || len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}

	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode success envelope: %w", err)
	}
	return env.Data, nil
}

// decodeAPIError maps a non-2xx response body ({"error":{code,message}}) into an
// *APIError. When the body is not a recognizable envelope, the raw body is used
// as the message so no detail is lost.
func decodeAPIError(status int, raw []byte) error {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && (env.Error.Code != "" || env.Error.Message != "") {
		return &APIError{Status: status, Code: env.Error.Code, Message: env.Error.Message}
	}
	msg := string(bytes.TrimSpace(raw))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &APIError{Status: status, Message: msg}
}
