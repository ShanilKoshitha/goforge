// Package validation contains composable, typed validation rules without
// reflection. Errors is the original field-message API; Report adds explicit
// input states, stable rule codes, nested paths, and application-defined rules.
package validation

import (
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"unicode/utf8"
)

type Errors map[string][]string

func (errors Errors) Error() string {
	count := 0
	for _, messages := range errors {
		count += len(messages)
	}
	return fmt.Sprintf("validation failed with %d error(s)", count)
}

func (errors Errors) Empty() bool { return len(errors) == 0 }

type Field interface {
	validate() (string, []string)
}

func Check(fields ...Field) Errors {
	errors := make(Errors)
	for _, field := range fields {
		name, messages := field.validate()
		if len(messages) > 0 {
			errors[name] = append(errors[name], messages...)
		}
	}
	return errors
}

type StringField struct {
	name     string
	value    string
	required bool
	minimum  int
	maximum  int
	email    bool
	allowed  []string
}

// String validates the original value. Blank values skip all rules unless Required is set.
func String(name, value string) *StringField {
	return &StringField{name: name, value: value}
}

func (field *StringField) Required() *StringField {
	field.required = true
	return field
}

// Min sets the minimum length in Unicode code points, including whitespace.
func (field *StringField) Min(length int) *StringField {
	field.minimum = length
	return field
}

// Max sets the maximum length in Unicode code points, including whitespace.
func (field *StringField) Max(length int) *StringField {
	field.maximum = length
	return field
}

func (field *StringField) Email() *StringField {
	field.email = true
	return field
}

func (field *StringField) OneOf(values ...string) *StringField {
	field.allowed = slices.Clone(values)
	return field
}

func (field *StringField) validate() (string, []string) {
	var messages []string
	value := strings.TrimSpace(field.value)
	if value == "" {
		if field.required {
			messages = append(messages, "is required")
		}
		return field.name, messages
	}
	length := utf8.RuneCountInString(field.value)
	if field.minimum > 0 && length < field.minimum {
		messages = append(messages, fmt.Sprintf("must be at least %d characters", field.minimum))
	}
	if field.maximum > 0 && length > field.maximum {
		messages = append(messages, fmt.Sprintf("must be at most %d characters", field.maximum))
	}
	if field.email {
		address, err := mail.ParseAddress(field.value)
		if err != nil || address.Address != field.value {
			messages = append(messages, "must be a valid email address")
		}
	}
	if len(field.allowed) > 0 && !slices.Contains(field.allowed, field.value) {
		messages = append(messages, "must be one of: "+strings.Join(field.allowed, ", "))
	}
	return field.name, messages
}

type IntField struct {
	name    string
	value   int
	minimum *int
	maximum *int
}

func Int(name string, value int) *IntField {
	return &IntField{name: name, value: value}
}

func (field *IntField) Min(value int) *IntField {
	field.minimum = &value
	return field
}

func (field *IntField) Max(value int) *IntField {
	field.maximum = &value
	return field
}

func (field *IntField) validate() (string, []string) {
	var messages []string
	if field.minimum != nil && field.value < *field.minimum {
		messages = append(messages, fmt.Sprintf("must be at least %d", *field.minimum))
	}
	if field.maximum != nil && field.value > *field.maximum {
		messages = append(messages, fmt.Sprintf("must be at most %d", *field.maximum))
	}
	return field.name, messages
}
