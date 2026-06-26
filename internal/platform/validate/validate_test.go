package validate

import (
	"errors"
	"testing"
)

type sample struct {
	Name  string `validate:"required"`
	Email string `validate:"required,email"`
	Age   int    `validate:"gte=0,lte=130"`
}

func TestValidate(t *testing.T) {
	t.Run("valid struct passes", func(t *testing.T) {
		err := Validate(sample{Name: "Ada", Email: "ada@example.com", Age: 30})
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("invalid struct yields field errors", func(t *testing.T) {
		err := Validate(sample{Email: "not-an-email", Age: 200})
		if err == nil {
			t.Fatal("expected error")
		}
		var verrs Errors
		if !errors.As(err, &verrs) {
			t.Fatalf("expected Errors, got %T: %v", err, err)
		}
		if len(verrs) < 3 {
			t.Errorf("expected at least 3 field errors, got %d: %v", len(verrs), verrs)
		}
		fields := map[string]bool{}
		for _, fe := range verrs {
			fields[fe.Field] = true
		}
		for _, want := range []string{"Name", "Email", "Age"} {
			if !fields[want] {
				t.Errorf("missing field error for %s", want)
			}
		}
	})
}
