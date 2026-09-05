package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/validation"
)

type RegisterRequest struct {
	Name                 string `json:"name" form:"name"`
	Email                string `json:"email" form:"email"`
	Password             string `json:"password" form:"password"`
	PasswordConfirmation string `json:"password_confirmation" form:"password_confirmation"`

	nameInput                 validation.Input[string]
	emailInput                validation.Input[string]
	passwordInput             validation.Input[string]
	passwordConfirmationInput validation.Input[string]
}

func (request *RegisterRequest) DecodeJSON(ctx *httpx.Context) error {
	var body struct {
		Name                 json.RawMessage `json:"name"`
		Email                json.RawMessage `json:"email"`
		Password             json.RawMessage `json:"password"`
		PasswordConfirmation json.RawMessage `json:"password_confirmation"`
	}
	if err := ctx.BindJSON(&body); err != nil {
		return err
	}
	var err error
	if request.Name, request.nameInput, err = decodeString("name", body.Name, strings.TrimSpace); err != nil {
		return err
	}
	if request.Email, request.emailInput, err = decodeString("email", body.Email, normalizeEmail); err != nil {
		return err
	}
	if request.Password, request.passwordInput, err = decodeString("password", body.Password, identity); err != nil {
		return err
	}
	if request.PasswordConfirmation, request.passwordConfirmationInput, err = decodeString("password_confirmation", body.PasswordConfirmation, identity); err != nil {
		return err
	}
	return nil
}

func (request *RegisterRequest) DecodeForm(ctx *httpx.Context) error {
	form, err := ctx.Form()
	if err != nil {
		return err
	}
	if request.Name, request.nameInput, err = formString(form, "name", strings.TrimSpace); err != nil {
		return err
	}
	if request.Email, request.emailInput, err = formString(form, "email", normalizeEmail); err != nil {
		return err
	}
	if request.Password, request.passwordInput, err = formString(form, "password", identity); err != nil {
		return err
	}
	if request.PasswordConfirmation, request.passwordConfirmationInput, err = formString(form, "password_confirmation", identity); err != nil {
		return err
	}
	return nil
}

func (request RegisterRequest) Validate() validation.Report {
	name := fallbackInput(request.nameInput, request.Name, strings.TrimSpace)
	email := fallbackInput(request.emailInput, request.Email, normalizeEmail)
	password := fallbackInput(request.passwordInput, request.Password, identity)
	confirmation := fallbackInput(request.passwordConfirmationInput, request.PasswordConfirmation, identity)
	confirmed := validation.RuleFunc[string](func(input validation.Input[string]) []validation.Issue {
		value, present := input.Value()
		original, originalPresent := password.Value()
		if present && originalPresent && value != original {
			return []validation.Issue{{Code: "password.confirmed", Message: "must match password"}}
		}
		return nil
	})
	return validation.Join(
		validation.Apply("name", name,
			validation.Required[string](), validation.StringLength(2, 100)),
		validation.Apply("email", email,
			validation.Required[string](), validation.StringLength(3, 320), emailRule()),
		validation.Apply("password", password,
			validation.Required[string](), validation.StringLength(12, 128)),
		validation.Apply("password_confirmation", confirmation,
			validation.Required[string](), confirmed),
	)
}

type LoginRequest struct {
	Email    string `json:"email" form:"email"`
	Password string `json:"password" form:"password"`

	emailInput    validation.Input[string]
	passwordInput validation.Input[string]
}

func (request *LoginRequest) DecodeJSON(ctx *httpx.Context) error {
	var body struct {
		Email    json.RawMessage `json:"email"`
		Password json.RawMessage `json:"password"`
	}
	if err := ctx.BindJSON(&body); err != nil {
		return err
	}
	var err error
	if request.Email, request.emailInput, err = decodeString("email", body.Email, normalizeEmail); err != nil {
		return err
	}
	if request.Password, request.passwordInput, err = decodeString("password", body.Password, identity); err != nil {
		return err
	}
	return nil
}

func (request *LoginRequest) DecodeForm(ctx *httpx.Context) error {
	form, err := ctx.Form()
	if err != nil {
		return err
	}
	if request.Email, request.emailInput, err = formString(form, "email", normalizeEmail); err != nil {
		return err
	}
	if request.Password, request.passwordInput, err = formString(form, "password", identity); err != nil {
		return err
	}
	return nil
}

func (request LoginRequest) Validate() validation.Report {
	email := fallbackInput(request.emailInput, request.Email, normalizeEmail)
	password := fallbackInput(request.passwordInput, request.Password, identity)
	return validation.Join(
		validation.Apply("email", email,
			validation.Required[string](), validation.StringLength(3, 320), emailRule()),
		validation.Apply("password", password,
			validation.Required[string](), validation.StringLength(0, 128)),
	)
}

func decodeString(field string, raw json.RawMessage, normalize func(string) string) (string, validation.Input[string], error) {
	if raw == nil {
		return "", validation.Missing[string](), nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", validation.Null[string](), nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", validation.Missing[string](), httpx.NewHTTPError(http.StatusBadRequest, field+" must be a string").WithCause(err)
	}
	value = normalize(value)
	if value == "" {
		return "", validation.Empty[string](), nil
	}
	return value, validation.Present(value), nil
}

func formString(form httpx.Form, field string, normalize func(string) string) (string, validation.Input[string], error) {
	value, err := form.Value(field)
	if err != nil {
		return "", validation.Missing[string](), err
	}
	if !form.Has(field) {
		return "", validation.Missing[string](), nil
	}
	value = normalize(value)
	if value == "" {
		return "", validation.Empty[string](), nil
	}
	return value, validation.Present(value), nil
}

func fallbackInput(input validation.Input[string], value string, normalize func(string) string) validation.Input[string] {
	if input.State() != validation.StateMissing || value == "" {
		return input
	}
	value = normalize(value)
	if value == "" {
		return validation.Empty[string]()
	}
	return validation.Present(value)
}

func emailRule() validation.Rule[string] {
	return validation.RuleFunc[string](func(input validation.Input[string]) []validation.Issue {
		value, present := input.Value()
		if !present {
			return nil
		}
		address, err := mail.ParseAddress(value)
		if err != nil || address.Address != value {
			return []validation.Issue{{Code: "string.email", Message: "must be a valid email address"}}
		}
		return nil
	})
}

func identity(value string) string { return value }
