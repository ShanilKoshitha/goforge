package httpx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type plainResponse struct {
	header   http.Header
	statuses []int
	body     bytes.Buffer
}

func (response *plainResponse) Header() http.Header {
	if response.header == nil {
		response.header = make(http.Header)
	}
	return response.header
}
func (response *plainResponse) Write(body []byte) (int, error) { return response.body.Write(body) }
func (response *plainResponse) WriteHeader(status int) {
	response.statuses = append(response.statuses, status)
}

type flushResponse struct {
	*plainResponse
	flushed bool
}

func (response *flushResponse) Flush() { response.flushed = true }

type richResponse struct {
	*plainResponse
	flushed bool
	pushed  string
}

type deadlineResponse struct {
	*plainResponse
	deadline time.Time
}

func (response *deadlineResponse) SetWriteDeadline(deadline time.Time) error {
	response.deadline = deadline
	return nil
}

func (response *richResponse) Flush() { response.flushed = true }
func (response *richResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("test hijack")
}
func (response *richResponse) Push(target string, _ *http.PushOptions) error {
	response.pushed = target
	return nil
}

func TestStatusWriterPreservesOptionalHTTPInterfacesExactly(t *testing.T) {
	plain := &plainResponse{}
	_, wrappedPlain := newStatusWriter(plain)
	if _, ok := wrappedPlain.(http.Flusher); ok {
		t.Fatal("plain writer unexpectedly advertises http.Flusher")
	}
	if _, ok := wrappedPlain.(http.Hijacker); ok {
		t.Fatal("plain writer unexpectedly advertises http.Hijacker")
	}
	if _, ok := wrappedPlain.(http.Pusher); ok {
		t.Fatal("plain writer unexpectedly advertises http.Pusher")
	}

	flushOnly := &flushResponse{plainResponse: &plainResponse{}}
	_, wrappedFlush := newStatusWriter(flushOnly)
	flusher, ok := wrappedFlush.(http.Flusher)
	if !ok {
		t.Fatal("http.Flusher was not preserved")
	}
	if _, ok := wrappedFlush.(http.Hijacker); ok {
		t.Fatal("flush-only writer unexpectedly advertises http.Hijacker")
	}
	flusher.Flush()
	if !flushOnly.flushed || len(flushOnly.statuses) != 1 || flushOnly.statuses[0] != http.StatusOK {
		t.Fatalf("flush did not commit status 200: flushed=%t statuses=%v", flushOnly.flushed, flushOnly.statuses)
	}

	rich := &richResponse{plainResponse: &plainResponse{}}
	_, wrappedRich := newStatusWriter(rich)
	if _, ok := wrappedRich.(http.Flusher); !ok {
		t.Fatal("rich writer lost http.Flusher")
	}
	if _, ok := wrappedRich.(http.Hijacker); !ok {
		t.Fatal("rich writer lost http.Hijacker")
	}
	pusher, ok := wrappedRich.(http.Pusher)
	if !ok {
		t.Fatal("rich writer lost http.Pusher")
	}
	if err := pusher.Push("/asset.css", nil); err != nil || rich.pushed != "/asset.css" {
		t.Fatalf("push was not delegated: target=%q err=%v", rich.pushed, err)
	}
}

func TestStatusWriterWorksWithResponseController(t *testing.T) {
	underlying := &flushResponse{plainResponse: &plainResponse{}}
	_, wrapped := newStatusWriter(underlying)
	if err := http.NewResponseController(wrapped).Flush(); err != nil {
		t.Fatal(err)
	}
	if !underlying.flushed {
		t.Fatal("ResponseController flush did not reach the underlying writer")
	}

	deadlineWriter := &deadlineResponse{plainResponse: &plainResponse{}}
	_, wrappedDeadline := newStatusWriter(deadlineWriter)
	want := time.Unix(123, 0)
	if err := http.NewResponseController(wrappedDeadline).SetWriteDeadline(want); err != nil {
		t.Fatal(err)
	}
	if !deadlineWriter.deadline.Equal(want) {
		t.Fatalf("ResponseController did not follow Unwrap: deadline=%s", deadlineWriter.deadline)
	}
}

func TestStatusWriterAllowsInformationalThenFinalStatus(t *testing.T) {
	underlying := &plainResponse{}
	tracker, _ := newStatusWriter(underlying)
	tracker.WriteHeader(http.StatusEarlyHints)
	if tracker.wroteHeader || tracker.status != http.StatusEarlyHints {
		t.Fatalf("informational response was treated as final: %+v", tracker)
	}
	tracker.WriteHeader(http.StatusCreated)
	tracker.WriteHeader(http.StatusAccepted)
	tracker.WriteHeader(http.StatusEarlyHints)
	if !tracker.wroteHeader || tracker.status != http.StatusCreated {
		t.Fatalf("final status was not tracked: %+v", tracker)
	}
	if len(underlying.statuses) != 2 || underlying.statuses[0] != http.StatusEarlyHints || underlying.statuses[1] != http.StatusCreated {
		t.Fatalf("unexpected delegated statuses %v", underlying.statuses)
	}
}

func TestStatusWriterTreatsSwitchingProtocolsAsFinal(t *testing.T) {
	underlying := &plainResponse{}
	tracker, _ := newStatusWriter(underlying)
	tracker.WriteHeader(http.StatusSwitchingProtocols)
	tracker.WriteHeader(http.StatusOK)
	if !tracker.Written() || tracker.status != http.StatusSwitchingProtocols {
		t.Fatalf("protocol switch was not committed: %+v", tracker)
	}
	if len(underlying.statuses) != 1 || underlying.statuses[0] != http.StatusSwitchingProtocols {
		t.Fatalf("unexpected delegated statuses %v", underlying.statuses)
	}
}

func TestNewCORSRejectsUnsafePolicyAndSnapshotsConfiguration(t *testing.T) {
	if _, err := NewCORS(CORSConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true}); err == nil {
		t.Fatal("credentialed wildcard CORS policy was accepted")
	}
	for _, config := range []CORSConfig{
		{AllowedOrigins: []string{"https://example.com/path"}},
		{AllowedOrigins: []string{"https://example.com"}, AllowedMethods: []string{"bad method"}},
		{AllowedOrigins: []string{"https://example.com"}, AllowedHeaders: []string{"bad header\n"}},
		{AllowedOrigins: []string{"https://example.com"}, MaxAge: -time.Second},
	} {
		if _, err := NewCORS(config); err == nil {
			t.Fatalf("invalid CORS policy was accepted: %+v", config)
		}
	}

	origins := []string{"https://allowed.example"}
	methods := []string{http.MethodPost}
	headers := []string{"X-Trace"}
	middleware, err := NewCORS(CORSConfig{AllowedOrigins: origins, AllowedMethods: methods, AllowedHeaders: headers})
	if err != nil {
		t.Fatal(err)
	}
	origins[0] = "https://mutated.example"
	methods[0] = http.MethodDelete
	headers[0] = "X-Mutated"

	router := NewRouter()
	router.Use(middleware)
	router.POST("/items", func(ctx *Context) error { return ctx.NoContent(http.StatusCreated) })
	request := httptest.NewRequest(http.MethodOptions, "/items", nil)
	request.Header.Set("Origin", "https://allowed.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "x-trace")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "https://allowed.example" {
		t.Fatalf("snapshotted policy response = %d headers=%v", response.Code, response.Header())
	}
	for _, value := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"} {
		if !headerListContains(response.Header().Values("Vary"), value) {
			t.Fatalf("Vary does not contain %q: %v", value, response.Header().Values("Vary"))
		}
	}

	denied := httptest.NewRequest(http.MethodOptions, "/items", nil)
	denied.Header.Set("Origin", "https://allowed.example")
	denied.Header.Set("Access-Control-Request-Method", http.MethodDelete)
	denied.Header.Set("Access-Control-Request-Headers", "X-Trace")
	deniedResponse := httptest.NewRecorder()
	router.ServeHTTP(deniedResponse, denied)
	if deniedResponse.Code != http.StatusForbidden || deniedResponse.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("denied preflight = %d headers=%v body=%s", deniedResponse.Code, deniedResponse.Header(), deniedResponse.Body.String())
	}
}

func TestLegacyCORSFailsEarlyForUnsafePolicy(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("CORS did not panic for an unsafe compatibility call")
		}
	}()
	_ = CORS(CORSConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true})
}

func TestRequestIDAcceptsOnlyOneBoundedSafeValue(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []string
		kept   bool
	}{
		{name: "valid", values: []string{"client-id_42:part.one"}, kept: true},
		{name: "space", values: []string{"not safe"}},
		{name: "oversized", values: []string{strings.Repeat("a", maximumRequestIDBytes+1)}},
		{name: "multiple", values: []string{"first", "second"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := NewRouter()
			router.Use(RequestID(""))
			router.GET("/", func(ctx *Context) error {
				if ctx.RequestID() == "" {
					t.Fatal("request ID accessor is empty")
				}
				return ctx.NoContent(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, value := range test.values {
				request.Header.Add("X-Request-ID", value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			got := response.Header().Get("X-Request-ID")
			if test.kept && got != test.values[0] {
				t.Fatalf("request ID = %q, want accepted %q", got, test.values[0])
			}
			if !test.kept && (got == "" || containsString(test.values, got) || len(got) != 32) {
				t.Fatalf("unsafe request ID was not replaced: %q from %v", got, test.values)
			}
		})
	}
}

func TestLoggerRecordsOneCorrelatedCompletionForEveryOutcome(t *testing.T) {
	tests := []struct {
		name        string
		handler     Handler
		middleware  []Middleware
		wantStatus  int
		wantOutcome string
	}{
		{name: "success", handler: func(ctx *Context) error { return ctx.NoContent(http.StatusCreated) }, wantStatus: http.StatusCreated, wantOutcome: "success"},
		{name: "HTTP error", handler: func(*Context) error { return NewHTTPError(http.StatusUnprocessableEntity, "invalid") }, wantStatus: http.StatusUnprocessableEntity, wantOutcome: "error"},
		{name: "canceled", handler: func(ctx *Context) error { return ctx.Request.Context().Err() }, wantStatus: http.StatusInternalServerError, wantOutcome: "canceled"},
		{name: "timeout", handler: func(ctx *Context) error { <-ctx.Request.Context().Done(); return ctx.Request.Context().Err() }, middleware: []Middleware{Timeout(time.Millisecond)}, wantStatus: http.StatusGatewayTimeout, wantOutcome: "timeout"},
		{name: "panic", handler: func(*Context) error { panic("secret panic value") }, wantStatus: http.StatusInternalServerError, wantOutcome: "panic"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			router := NewRouter()
			router.Use(Recover(logger), RequestID(""), Logger(logger))
			for _, middleware := range test.middleware {
				router.Use(middleware)
			}
			router.Named("items.show", http.MethodGet, "/items/{id}", test.handler)
			request := httptest.NewRequest(http.MethodGet, "/items/42", nil)
			if test.name == "canceled" {
				ctx, cancel := context.WithCancel(request.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			var completions []map[string]any
			for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatalf("decode log %q: %v", line, err)
				}
				if record["msg"] == "request completed" {
					completions = append(completions, record)
				}
			}
			if len(completions) != 1 {
				t.Fatalf("completion records = %d; logs=%s", len(completions), output.String())
			}
			record := completions[0]
			if record["request_id"] == "" || record["method"] != http.MethodGet || record["route"] != "/items/{id}" || record["outcome"] != test.wantOutcome || int(record["status"].(float64)) != test.wantStatus {
				t.Fatalf("completion record = %#v", record)
			}
			if strings.Contains(output.String(), "secret panic value") {
				t.Fatalf("panic value leaked to logs: %s", output.String())
			}
		})
	}
}

func TestValidatedTimeoutAndSecurityHeaders(t *testing.T) {
	if _, err := NewTimeout(0); err == nil {
		t.Fatal("zero timeout was accepted")
	}
	if _, err := NewSecureHeaders(SecurityHeadersConfig{ContentSecurityPolicy: "ok\r\nbad"}); err == nil {
		t.Fatal("header injection policy was accepted")
	}
	middleware, err := NewSecureHeaders(SecurityHeadersConfig{
		ContentSecurityPolicy:   "default-src 'self'",
		StrictTransportSecurity: "max-age=31536000",
	})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter()
	router.Use(middleware)
	router.GET("/", func(ctx *Context) error { return ctx.NoContent(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Header().Get("Content-Security-Policy") != "default-src 'self'" || response.Header().Get("Strict-Transport-Security") != "max-age=31536000" {
		t.Fatalf("security headers = %v", response.Header())
	}
}

func headerListContains(lines []string, sought string) bool {
	for _, line := range lines {
		for _, value := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(value), sought) {
				return true
			}
		}
	}
	return false
}

func containsString(values []string, sought string) bool {
	for _, value := range values {
		if value == sought {
			return true
		}
	}
	return false
}
