// Package api is the REST API surface (M17-1): a stateless, bearer-token
// authenticated JSON API mounted at /api/v1. It is the foundation a later MCP
// server consumes. Handlers are thin: they parse the request, call an existing
// content service through a narrow reader interface, and map the result onto a
// stable DTO wrapped in a uniform JSON envelope. No business logic lives here.
//
// The package imports internal/web (for UserFromContext and the shared
// *web.AuthMiddleware) in a strict one-way direction; internal/web must never
// import internal/api.
package api

import (
	"net/http"

	"github.com/huseyn0w/cmstack-go/internal/platform/httpx"
)

// OK writes a success envelope: {"data": <data>} with the given status.
func OK(w http.ResponseWriter, status int, data any) {
	httpx.JSON(w, status, envelope{Data: data})
}

// Fail writes an error envelope: {"error":{"code":..,"message":..}} with the
// given status. code is a stable machine-readable token (e.g. "not_found");
// message is a human-readable explanation.
func Fail(w http.ResponseWriter, status int, code, message string) {
	httpx.JSON(w, status, errEnvelope{Error: apiError{Code: code, Message: message}})
}

// FailValidation writes a 422 validation-error envelope carrying per-field
// messages: {"error":{"code":"validation","message":..,"fields":{...}}}.
func FailValidation(w http.ResponseWriter, fields map[string]string) {
	httpx.JSON(w, http.StatusUnprocessableEntity, errEnvelope{Error: apiError{
		Code:    "validation",
		Message: "the request failed validation",
		Fields:  fields,
	}})
}

// envelope is the success wrapper.
type envelope struct {
	Data any `json:"data"`
}

// errEnvelope is the error wrapper.
type errEnvelope struct {
	Error apiError `json:"error"`
}

// apiError is the error body: a stable code, a human message, and optional
// per-field validation messages.
type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}
