package accounts

// registerDTO is the validated signup request. Field names map to the form
// inputs and to the error-summary anchors.
type registerDTO struct {
	Name     string `validate:"required,min=1,max=120"`
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
	"Email":    "email",
	"Password": "password",
}
