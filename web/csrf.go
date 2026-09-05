package web

import (
	"errors"
	"fmt"
	"mime"
	"net/http"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/security/csrf"
)

const (
	// CSRFField is the conventional hidden form field read by CSRF middleware.
	CSRFField = "_token"
	// CSRFHeader is accepted for JavaScript requests that cannot submit a form field.
	CSRFHeader = "X-CSRF-Token"
)

// CSRF protects every method other than GET, HEAD, and OPTIONS with a
// session-bound synchronizer token. URL-encoded forms submit _token in their
// body; other clients submit X-CSRF-Token. Query parameters are never read.
// Sessions middleware must be installed before CSRF.
func CSRF() httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx *httpx.Context) error {
			if csrfSafeMethod(ctx.Request.Method) {
				return next(ctx)
			}
			current, ok := Session(ctx)
			if !ok {
				return fmt.Errorf("web: CSRF middleware requires session middleware")
			}
			submitted, err := submittedCSRFToken(ctx)
			if err != nil {
				return err
			}
			if err := csrf.Validate(current, submitted); err != nil {
				if errors.Is(err, csrf.ErrInvalidToken) {
					return httpx.NewHTTPError(http.StatusForbidden, "invalid CSRF token")
				}
				return fmt.Errorf("validate CSRF token: %w", err)
			}
			return next(ctx)
		}
	}
}

// CSRFToken returns the current token, creating it in the request session when
// necessary. Sessions middleware persists it before the rendered response is
// committed.
func CSRFToken(ctx *httpx.Context) (string, error) {
	current, ok := Session(ctx)
	if !ok {
		return "", fmt.Errorf("web: CSRF token requires session middleware")
	}
	return csrf.Ensure(current)
}

// RotateCSRF replaces the request session's token. Use it when authentication
// state changes; Sessions middleware persists it atomically with session ID
// regeneration before committing the response.
func RotateCSRF(ctx *httpx.Context) (string, error) {
	current, ok := Session(ctx)
	if !ok {
		return "", fmt.Errorf("web: CSRF rotation requires session middleware")
	}
	return csrf.Rotate(current)
}

func submittedCSRFToken(ctx *httpx.Context) (string, error) {
	values := ctx.Request.Header.Values(CSRFHeader)
	if len(values) > 1 {
		return "", httpx.NewHTTPError(http.StatusBadRequest, "CSRF header must appear at most once")
	}
	if len(values) == 1 && values[0] != "" {
		return values[0], nil
	}
	mediaType, _, err := mime.ParseMediaType(ctx.Request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return "", nil
	}
	form, err := ctx.Form()
	if err != nil {
		return "", err
	}
	return form.Value(CSRFField)
}

func csrfSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
