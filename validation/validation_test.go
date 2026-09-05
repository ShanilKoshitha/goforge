package validation_test

import (
	"testing"

	"github.com/ShanilKoshitha/goforge/validation"
)

func TestCheckReturnsErrorsByField(t *testing.T) {
	errors := validation.Check(
		validation.String("name", "").Required().Min(2),
		validation.String("email", "not-an-email").Required().Email(),
		validation.String("role", "owner").OneOf("admin", "member"),
		validation.Int("age", 12).Min(18),
	)
	for _, field := range []string{"name", "email", "role", "age"} {
		if len(errors[field]) == 0 {
			t.Errorf("expected an error for %s", field)
		}
	}
}

func TestOptionalEmptyStringSkipsOtherRules(t *testing.T) {
	errors := validation.Check(validation.String("nickname", "").Min(10).Email())
	if !errors.Empty() {
		t.Fatalf("expected no errors, got %v", errors)
	}
}

func TestStringLengthCountsUnicodeCodePoints(t *testing.T) {
	for _, value := range []string{"go", "日本", "☕🚀"} {
		if errs := validation.Check(validation.String("name", value).Min(2).Max(2)); !errs.Empty() {
			t.Errorf("two code points %q failed validation: %v", value, errs)
		}
	}
}

func TestOneOfCopiesAllowedValues(t *testing.T) {
	allowed := []string{"member"}
	field := validation.String("role", "admin").OneOf(allowed...)
	allowed[0] = "admin"
	if errs := validation.Check(field); errs.Empty() {
		t.Fatal("mutating the caller's slice changed the validation rule")
	}
}
