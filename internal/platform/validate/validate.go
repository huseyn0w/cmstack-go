// Package validate wraps go-playground/validator with a small, stable API.
package validate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

// validatorInstance is safe for concurrent use and caches struct metadata.
var validatorInstance = validator.New(validator.WithRequiredStructEnabled())

// FieldError describes a single failed validation rule.
type FieldError struct {
	Field string `json:"field"`
	Tag   string `json:"tag"`
}

func (e FieldError) Error() string {
	return fmt.Sprintf("%s failed %q rule", e.Field, e.Tag)
}

// Errors is a collection of field errors returned by Validate.
type Errors []FieldError

func (e Errors) Error() string {
	parts := make([]string, len(e))
	for i, fe := range e {
		parts[i] = fe.Error()
	}
	return strings.Join(parts, "; ")
}

// Validate validates v's struct tags. It returns nil when v is valid, or an
// Errors value describing every violated rule.
func Validate(v any) error {
	err := validatorInstance.Struct(v)
	if err == nil {
		return nil
	}

	var invalid *validator.InvalidValidationError
	if errors.As(err, &invalid) {
		return fmt.Errorf("validate: %w", err)
	}

	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) {
		out := make(Errors, 0, len(verrs))
		for _, fe := range verrs {
			out = append(out, FieldError{Field: fe.Field(), Tag: fe.Tag()})
		}
		return out
	}

	return err
}
