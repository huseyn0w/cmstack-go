package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// maxBodyBytes caps a decoded JSON request body so a single call cannot exhaust
// memory. Write payloads here are small (content text lives in a few fields).
const maxBodyBytes = 1 << 20 // 1 MiB

// errEmptyBody is the sentinel DecodeJSON returns when the request carries no
// body at all. Handlers map it to a 400 with a clear message.
var errEmptyBody = errors.New("api: empty request body")

// DecodeJSON strictly decodes the request body into dst. It caps the body size,
// rejects unknown fields, and rejects trailing content after the first JSON
// value, so a malformed or over-broad payload is refused rather than silently
// accepted. The returned error is opaque to the caller beyond errEmptyBody; the
// write handlers turn any decode failure into a clean 400/422.
func DecodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return errEmptyBody
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errEmptyBody
		}
		return err
	}
	// Reject any trailing tokens after the first JSON value (e.g. two objects).
	if dec.More() {
		return errors.New("api: unexpected trailing content in request body")
	}
	return nil
}

// failBadJSON writes the uniform 400 for a body that failed to decode. An empty
// body gets a distinct message so the client knows a payload was required.
func failBadJSON(w http.ResponseWriter, err error) {
	if errors.Is(err, errEmptyBody) {
		Fail(w, http.StatusBadRequest, "invalid_body", "a JSON request body is required")
		return
	}
	Fail(w, http.StatusBadRequest, "invalid_body", "the request body is not valid JSON")
}

// idParam parses the {id} URL parameter as a UUID. On failure it writes the
// uniform 400 and returns ok=false so the caller returns immediately.
func idParam(w http.ResponseWriter, r *http.Request, subject string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Fail(w, http.StatusBadRequest, "invalid_id", "the "+subject+" id is not a valid uuid")
		return uuid.Nil, false
	}
	return id, true
}

// parseUUIDs converts a slice of string ids into UUIDs, returning an error on
// the first malformed value. It is used to translate the JSON string-id arrays
// (categoryIds/tagIds) into the domain uuid.UUID slices.
func parseUUIDs(raw []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}
