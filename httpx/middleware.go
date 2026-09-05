package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/textproto"
	"net/url"
	"runtime/debug"
	"strings"
	"time"
)

const maximumRequestIDBytes = 128

func Recover(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next Handler) Handler {
		return func(ctx *Context) (err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					if recovered == http.ErrAbortHandler {
						panic(recovered)
					}
					logger.Error("request panic recovered",
						"request_id", ctx.RequestID(),
						"method", ctx.Request.Method,
						"route", ctx.RoutePattern(),
						"stack", string(debug.Stack()),
					)
					err = NewHTTPError(http.StatusInternalServerError, "internal server error")
				}
			}()
			return next(ctx)
		}
	}
}

func RequestID(header string) Middleware {
	if header == "" {
		header = "X-Request-ID"
	}
	header = textproto.CanonicalMIMEHeaderKey(header)
	if header == "" {
		panic("httpx: request ID header is invalid")
	}
	return func(next Handler) Handler {
		return func(ctx *Context) error {
			values := ctx.Request.Header.Values(header)
			requestID := ""
			if len(values) == 1 && validRequestID(values[0]) {
				requestID = values[0]
			}
			if requestID == "" {
				var bytes [16]byte
				if _, err := rand.Read(bytes[:]); err != nil {
					return fmt.Errorf("generate request ID: %w", err)
				}
				requestID = hex.EncodeToString(bytes[:])
			}
			ctx.requestID = requestID
			// Retain the pre-v0.7 string lookup while new code uses Context.RequestID.
			ctx.Set("request_id", requestID)
			ctx.Response.Header().Set(header, requestID)
			return next(ctx)
		}
	}
}

func validRequestID(value string) bool {
	if value == "" || len(value) > maximumRequestIDBytes {
		return false
	}
	for _, character := range []byte(value) {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("._:-", rune(character)) {
			continue
		}
		return false
	}
	return true
}

func Logger(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next Handler) Handler {
		return func(ctx *Context) (err error) {
			started := time.Now()
			// Capture the inbound context before inner timeout middleware swaps it
			// for a child and cancels that child during normal cleanup.
			requestContext := ctx.Request.Context()
			defer func() {
				recovered := recover()
				status := ctx.response.status
				outcome := "success"
				switch {
				case recovered != nil:
					outcome = "panic"
					if !ctx.response.Written() {
						status = http.StatusInternalServerError
					}
				case errors.Is(err, context.DeadlineExceeded) || errors.Is(requestContext.Err(), context.DeadlineExceeded):
					outcome = "timeout"
					if !ctx.response.Written() {
						status = http.StatusGatewayTimeout
					}
				case errors.Is(err, context.Canceled) || errors.Is(requestContext.Err(), context.Canceled):
					outcome = "canceled"
					if !ctx.response.Written() {
						status = http.StatusInternalServerError
					}
				case err != nil:
					outcome = "error"
					var httpError *HTTPError
					if errors.As(err, &httpError) && httpError.Status >= 400 && httpError.Status <= 599 {
						status = httpError.Status
					} else if !ctx.response.Written() {
						status = http.StatusInternalServerError
					}
				}
				logger.Info("request completed",
					"request_id", ctx.RequestID(),
					"method", ctx.Request.Method,
					"route", ctx.RoutePattern(),
					"status", status,
					"duration", time.Since(started),
					"outcome", outcome,
				)
				if recovered != nil {
					panic(recovered)
				}
			}()
			err = next(ctx)
			return err
		}
	}
}

// Timeout sets a request context deadline. Handlers must observe cancellation;
// it does not interrupt handlers or write a timeout response on their behalf.
func Timeout(duration time.Duration) Middleware {
	middleware, err := NewTimeout(duration)
	if err != nil {
		panic(err)
	}
	return middleware
}

// NewTimeout constructs cooperative deadline middleware. Handlers must still
// observe cancellation; custom streaming servers can omit this middleware.
func NewTimeout(duration time.Duration) (Middleware, error) {
	if duration <= 0 {
		return nil, fmt.Errorf("httpx: timeout must be positive")
	}
	return func(next Handler) Handler {
		return func(ctx *Context) error {
			requestContext, cancel := context.WithTimeout(ctx.Request.Context(), duration)
			defer cancel()
			ctx.Request = ctx.Request.WithContext(requestContext)
			err := next(ctx)
			if errors.Is(err, context.DeadlineExceeded) || requestContext.Err() == context.DeadlineExceeded {
				return NewHTTPError(http.StatusGatewayTimeout, "request timed out").WithCause(context.DeadlineExceeded)
			}
			return err
		}
	}, nil
}

func SecureHeaders() Middleware {
	middleware, _ := NewSecureHeaders(SecurityHeadersConfig{})
	return middleware
}

type SecurityHeadersConfig struct {
	ContentSecurityPolicy   string
	StrictTransportSecurity string
}

// NewSecureHeaders validates optional policy values before serving. HSTS is
// deliberately opt-in because applications may terminate TLS upstream or run
// local plain HTTP.
func NewSecureHeaders(config SecurityHeadersConfig) (Middleware, error) {
	if !validResponseHeaderValue(config.ContentSecurityPolicy) {
		return nil, fmt.Errorf("httpx: content security policy contains a line break")
	}
	if !validResponseHeaderValue(config.StrictTransportSecurity) {
		return nil, fmt.Errorf("httpx: strict transport security policy contains a line break")
	}
	return func(next Handler) Handler {
		return func(ctx *Context) error {
			headers := ctx.Response.Header()
			headers.Set("X-Content-Type-Options", "nosniff")
			headers.Set("X-Frame-Options", "DENY")
			headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			if config.ContentSecurityPolicy != "" {
				headers.Set("Content-Security-Policy", config.ContentSecurityPolicy)
			}
			if config.StrictTransportSecurity != "" {
				headers.Set("Strict-Transport-Security", config.StrictTransportSecurity)
			}
			return next(ctx)
		}
	}, nil
}

func validResponseHeaderValue(value string) bool {
	return !strings.ContainsAny(value, "\r\n")
}

type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           time.Duration
}

func CORS(config CORSConfig) Middleware {
	middleware, err := NewCORS(config)
	if err != nil {
		panic(err)
	}
	return middleware
}

// NewCORS validates and snapshots cross-origin policy before serving.
func NewCORS(config CORSConfig) (Middleware, error) {
	prepared, err := prepareCORS(config)
	if err != nil {
		return nil, err
	}
	return func(next Handler) Handler {
		return func(ctx *Context) error {
			headers := ctx.Response.Header()
			preflight := ctx.Request.Method == http.MethodOptions && ctx.Request.Header.Get("Access-Control-Request-Method") != ""
			if preflight {
				addVary(headers, "Access-Control-Request-Method")
				addVary(headers, "Access-Control-Request-Headers")
			}
			wildcard := contains(prepared.AllowedOrigins, "*")
			if !wildcard {
				// Caches must also distinguish requests with an absent or denied origin.
				addVary(headers, "Origin")
			}
			origin := ctx.Request.Header.Get("Origin")
			if origin == "" || !originAllowed(origin, prepared.AllowedOrigins) {
				return next(ctx)
			}
			if preflight {
				if !contains(prepared.AllowedMethods, strings.ToUpper(ctx.Request.Header.Get("Access-Control-Request-Method"))) ||
					!requestedHeadersAllowed(ctx.Request.Header.Values("Access-Control-Request-Headers"), prepared.AllowedHeaders) {
					return NewHTTPError(http.StatusForbidden, "CORS preflight is not allowed")
				}
			}
			if wildcard {
				headers.Set("Access-Control-Allow-Origin", "*")
			} else {
				headers.Set("Access-Control-Allow-Origin", origin)
			}
			headers.Set("Access-Control-Allow-Methods", strings.Join(prepared.AllowedMethods, ", "))
			if len(prepared.AllowedHeaders) > 0 {
				headers.Set("Access-Control-Allow-Headers", strings.Join(prepared.AllowedHeaders, ", "))
			}
			if len(prepared.ExposeHeaders) > 0 {
				headers.Set("Access-Control-Expose-Headers", strings.Join(prepared.ExposeHeaders, ", "))
			}
			if prepared.AllowCredentials {
				headers.Set("Access-Control-Allow-Credentials", "true")
			}
			if prepared.MaxAge > 0 {
				headers.Set("Access-Control-Max-Age", fmt.Sprintf("%.0f", prepared.MaxAge.Seconds()))
			}
			if preflight {
				return ctx.NoContent(http.StatusNoContent)
			}
			return next(ctx)
		}
	}, nil
}

func prepareCORS(config CORSConfig) (CORSConfig, error) {
	prepared := CORSConfig{
		AllowedOrigins:   append([]string(nil), config.AllowedOrigins...),
		AllowedMethods:   append([]string(nil), config.AllowedMethods...),
		AllowedHeaders:   append([]string(nil), config.AllowedHeaders...),
		ExposeHeaders:    append([]string(nil), config.ExposeHeaders...),
		AllowCredentials: config.AllowCredentials,
		MaxAge:           config.MaxAge,
	}
	if prepared.MaxAge < 0 {
		return CORSConfig{}, fmt.Errorf("httpx: CORS max age cannot be negative")
	}
	if len(prepared.AllowedMethods) == 0 {
		prepared.AllowedMethods = []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions}
	}
	for index, origin := range prepared.AllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "*" {
			if prepared.AllowCredentials {
				return CORSConfig{}, fmt.Errorf("httpx: CORS wildcard origin cannot allow credentials")
			}
			prepared.AllowedOrigins[index] = origin
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return CORSConfig{}, fmt.Errorf("httpx: invalid CORS origin %q", origin)
		}
		prepared.AllowedOrigins[index] = origin
	}
	for index, method := range prepared.AllowedMethods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if !validHTTPToken(method) {
			return CORSConfig{}, fmt.Errorf("httpx: invalid CORS method %q", method)
		}
		prepared.AllowedMethods[index] = method
	}
	for _, collection := range [][]string{prepared.AllowedHeaders, prepared.ExposeHeaders} {
		for index, header := range collection {
			header = strings.TrimSpace(header)
			if !validHTTPToken(header) {
				return CORSConfig{}, fmt.Errorf("httpx: invalid CORS header %q", header)
			}
			collection[index] = textproto.CanonicalMIMEHeaderKey(header)
		}
	}
	return prepared, nil
}

func requestedHeadersAllowed(lines, allowed []string) bool {
	for _, line := range lines {
		for _, requested := range strings.Split(line, ",") {
			requested = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(requested))
			if requested == "" || !containsFold(allowed, requested) {
				return false
			}
		}
	}
	return true
}

func validHTTPToken(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character <= 32 || character >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={} \t", character) {
			return false
		}
	}
	return true
}

func addVary(headers http.Header, value string) {
	for _, line := range headers.Values("Vary") {
		for _, existing := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(existing), value) {
				return
			}
		}
	}
	headers.Add("Vary", value)
}

func containsFold(values []string, sought string) bool {
	for _, value := range values {
		if strings.EqualFold(value, sought) {
			return true
		}
	}
	return false
}

func originAllowed(origin string, allowed []string) bool {
	return contains(allowed, "*") || contains(allowed, origin)
}

func contains(values []string, sought string) bool {
	for _, value := range values {
		if value == sought {
			return true
		}
	}
	return false
}
