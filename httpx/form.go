package httpx

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

const formMediaType = "application/x-www-form-urlencoded"

// Form contains URL-encoded request-body values. Query parameters are never
// included. Value rejects repeated fields so scalar binding cannot silently
// discard attacker-controlled values; use Values for deliberately repeated
// fields.
type Form struct {
	values url.Values
}

func (form Form) Value(name string) (string, error) {
	values := form.values[name]
	if len(values) > 1 {
		return "", NewHTTPError(http.StatusBadRequest, fmt.Sprintf("form field %q must appear at most once", name))
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], nil
}

func (form Form) Values(name string) []string {
	return slices.Clone(form.values[name])
}

func (form Form) Has(name string) bool {
	_, ok := form.values[name]
	return ok
}

// Form parses an application/x-www-form-urlencoded body up to 1 MiB.
func (ctx *Context) Form() (Form, error) {
	return ctx.FormLimit(defaultMaxBodyBytes)
}

// FormLimit parses an application/x-www-form-urlencoded body up to maxBytes.
// Parsing is cached per request, and only body values from Request.PostForm are
// exposed.
func (ctx *Context) FormLimit(maxBytes int64) (Form, error) {
	if maxBytes <= 0 {
		return Form{}, fmt.Errorf("form body limit must be positive")
	}
	if ctx.formParsed {
		if ctx.formErr == nil && ctx.formBytes > maxBytes {
			return Form{}, NewHTTPError(http.StatusRequestEntityTooLarge, "form body is too large")
		}
		return Form{values: ctx.form}, ctx.formErr
	}
	ctx.formParsed = true

	mediaType, _, err := mime.ParseMediaType(ctx.Request.Header.Get("Content-Type"))
	if err != nil || mediaType != formMediaType {
		ctx.formErr = NewHTTPError(http.StatusUnsupportedMediaType, "Content-Type must be application/x-www-form-urlencoded")
		return Form{}, ctx.formErr
	}

	body := ctx.Request.Body
	if body == nil {
		body = http.NoBody
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Response, body, maxBytes)
	encoded, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			ctx.formErr = NewHTTPError(http.StatusRequestEntityTooLarge, "form body is too large").WithCause(err)
		} else {
			ctx.formErr = NewHTTPError(http.StatusBadRequest, "invalid URL-encoded form body").WithCause(err)
		}
		return Form{}, ctx.formErr
	}
	values, err := url.ParseQuery(string(encoded))
	if err != nil {
		ctx.formErr = NewHTTPError(http.StatusBadRequest, "invalid URL-encoded form body").WithCause(err)
		return Form{}, ctx.formErr
	}
	ctx.formBytes = int64(len(encoded))
	ctx.Request.PostForm = cloneValues(values)
	ctx.form = cloneValues(values)
	return Form{values: ctx.form}, nil
}

// MethodOverride allows URL-encoded POST forms to target PUT, PATCH, or DELETE
// routes through a scalar _method body field. When prefixes are supplied, only
// matching request paths are eligible. Applications serving cookie-authenticated
// APIs should scope override to their CSRF-protected browser route prefix.
func MethodOverride(prefixes ...string) Middleware {
	allowedPath := func(requestPath string) bool {
		if len(prefixes) == 0 {
			return true
		}
		for _, prefix := range prefixes {
			if prefix != "" && strings.HasPrefix(requestPath, prefix) {
				return true
			}
		}
		return false
	}
	return func(next Handler) Handler {
		return func(ctx *Context) error {
			if ctx.Request.Method != http.MethodPost || !allowedPath(ctx.Request.URL.Path) || !isURLForm(ctx.Request.Header.Get("Content-Type")) {
				return next(ctx)
			}
			form, err := ctx.Form()
			if err != nil {
				return err
			}
			method, err := form.Value("_method")
			if err != nil {
				return err
			}
			if method == "" {
				return next(ctx)
			}
			method = strings.ToUpper(strings.TrimSpace(method))
			switch method {
			case http.MethodPut, http.MethodPatch, http.MethodDelete:
				ctx.Request.Method = method
			default:
				return NewHTTPError(http.StatusBadRequest, "form method override must be PUT, PATCH, or DELETE")
			}
			return next(ctx)
		}
	}
}

func isURLForm(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == formMediaType
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for name, entries := range values {
		cloned[name] = slices.Clone(entries)
	}
	return cloned
}
