// Package httpx contains small HTTP boundary helpers shared by handlers.
package httpx

import (
	"encoding/json"
	"net/http"
)

// JSON writes v as an application/json response with the given status code.
// It is intentionally tiny: handlers stay thin and never hand-roll encoding.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// A failed encode after WriteHeader cannot be recovered into the response;
	// it is logged by the recoverer/logging middleware if it panics. We ignore
	// the error here deliberately to keep the helper allocation-free of state.
	_ = json.NewEncoder(w).Encode(v)
}
