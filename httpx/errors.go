package httpx

import (
	"errors"
	"fmt"
	"net/http"
)

type HTTPError struct {
	Status  int
	Message string
	Details any
	Cause   error
}

func NewHTTPError(status int, message string) *HTTPError {
	return &HTTPError{Status: status, Message: message}
}

func (err *HTTPError) Error() string {
	if err.Cause != nil {
		return fmt.Sprintf("%s: %v", err.Message, err.Cause)
	}
	return err.Message
}

func (err *HTTPError) Unwrap() error { return err.Cause }

func (err *HTTPError) WithDetails(details any) *HTTPError {
	err.Details = details
	return err
}

func (err *HTTPError) WithCause(cause error) *HTTPError {
	err.Cause = cause
	return err
}

type ErrorHandler func(*Context, error)

func DefaultErrorHandler(ctx *Context, err error) {
	status := http.StatusInternalServerError
	message := "internal server error"
	var details any
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		status = httpError.Status
		message = httpError.Message
		details = httpError.Details
	}
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
		message = "internal server error"
		details = nil
	}
	payload := map[string]any{"message": message}
	if details != nil {
		payload["details"] = details
	}
	_ = ctx.JSON(status, map[string]any{"error": payload})
}
