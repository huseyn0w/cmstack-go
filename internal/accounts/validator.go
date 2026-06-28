package accounts

import (
	"errors"

	"github.com/huseyn0w/cmstack-go/internal/platform/validate"
)

// NewValidator returns a Validator that runs struct-tag validation and maps
// failures to lowercase form-field names with human-readable messages, suitable
// for the templ error summary (which links each message to its input by name).
func NewValidator() Validator {
	return func(v any) map[string]string {
		err := validate.Validate(v)
		if err == nil {
			return nil
		}
		var verrs validate.Errors
		if !errors.As(err, &verrs) {
			// Non-field error: surface generically under a synthetic key.
			return map[string]string{"form": err.Error()}
		}
		out := make(map[string]string, len(verrs))
		for _, fe := range verrs {
			name := dtoFieldNames[fe.Field]
			if name == "" {
				name = fe.Field
			}
			out[name] = messageFor(fe.Field, fe.Tag)
		}
		return out
	}
}

func messageFor(field, tag string) string {
	switch tag {
	case "required":
		return field + " is required."
	case "email":
		return "Enter a valid email address."
	case "min":
		if field == "Password" {
			return "Password must be at least 8 characters."
		}
		return field + " is too short."
	case "max":
		return field + " is too long."
	default:
		return field + " is invalid."
	}
}
