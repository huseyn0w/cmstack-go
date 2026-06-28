package accounts

// registerDTO is the validated signup request. Field names map to the form
// inputs and to the error-summary anchors.
type registerDTO struct {
	Name string `validate:"required,min=1,max=120"`
	// Username is OPTIONAL: empty is valid (email stays the only identifier). When
	// present it must be a 3–30 char handle. The authoritative format + uniqueness
	// check lives in the service (validUsername); this tag is just a length guard
	// so the form gives an early, friendly error.
	Username string `validate:"omitempty,min=3,max=30"`
	Email    string `validate:"required,email,max=254"`
	Password string `validate:"required,min=8,max=128"`
}

// resetDTO is the validated new-password request.
type resetDTO struct {
	Password string `validate:"required,min=8,max=128"`
}

// dtoFieldNames maps struct field names to the lowercase form field/anchor name
// so validation errors link to the correct input.
var dtoFieldNames = map[string]string{
	"Name":     "name",
	"Username": "username",
	"Email":    "email",
	"Password": "password",
}
