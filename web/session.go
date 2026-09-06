// Package web provides the buffered request lifecycle used by browser routes.
// Streaming, server-sent event, WebSocket, and large download routes should be
// registered outside this middleware and use the lower-level packages directly.
package web

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ShanilKoshitha/goforge/httpx"
	forgesession "github.com/ShanilKoshitha/goforge/session"
)

const (
	sessionContextKey               = "goforge.web.session"
	defaultMaxBufferedResponseBytes = 4 << 20
)

var ErrBufferedResponseTooLarge = errors.New("web: buffered response exceeds configured limit")

type sessionState struct {
	manager   *forgesession.Manager
	current   *forgesession.Session
	accessed  bool
	destroyed bool
}

// Sessions loads one server-side session for each request. Responses are held
// until an accessed session has been saved, so cookies and redirects are never
// committed ahead of session persistence. Anonymous sessions that handlers do
// not access are neither created in the store nor written as cookies.
//
// The middleware deliberately buffers browser responses and does not expose
// streaming interfaces. Register streaming routes outside it.
func Sessions(manager *forgesession.Manager) httpx.Middleware {
	return SessionsLimit(manager, defaultMaxBufferedResponseBytes)
}

// SessionsLimit is Sessions with an explicit maximum buffered response size.
// Register downloads and streaming endpoints outside this middleware.
func SessionsLimit(manager *forgesession.Manager, maxResponseBytes int64) httpx.Middleware {
	if manager == nil {
		panic("web: session manager is required")
	}
	if maxResponseBytes <= 0 {
		panic("web: buffered response limit must be positive")
	}
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx *httpx.Context) error {
			current, err := manager.Load(ctx.Request.Context(), ctx.Request)
			if err != nil {
				return fmt.Errorf("load browser session: %w", err)
			}
			state := &sessionState{manager: manager, current: current}
			ctx.Set(sessionContextKey, state)

			original := ctx.Response
			buffer := newResponseBuffer(original.Header(), maxResponseBytes)
			ctx.Response = buffer
			defer func() { ctx.Response = original }()
			err = next(ctx)
			if err != nil {
				return err
			}
			if buffer.err != nil {
				return buffer.err
			}
			if state.accessed && !state.destroyed {
				if err := manager.Save(ctx.Request.Context(), buffer, current); err != nil {
					return fmt.Errorf("persist browser session: %w", err)
				}
			}
			if state.accessed {
				buffer.Header().Set("Cache-Control", "no-store")
			}
			if err := buffer.commit(original); err != nil {
				return fmt.Errorf("commit browser response: %w", err)
			}
			return nil
		}
	}
}

// Session returns the request's session and marks it for persistence. The
// boolean is false when Sessions middleware is not installed.
func Session(ctx *httpx.Context) (*forgesession.Session, bool) {
	state, ok := stateFrom(ctx)
	if !ok {
		return nil, false
	}
	state.accessed = true
	return state.current, true
}

// Regenerate stages a new session ID. Sessions middleware atomically persists
// the replacement before committing the response.
func Regenerate(ctx *httpx.Context) error {
	state, ok := stateFrom(ctx)
	if !ok {
		return fmt.Errorf("web: session middleware is not installed")
	}
	state.accessed = true
	return state.manager.Regenerate(ctx.Request.Context(), state.current)
}

// RegenerateOrCreate is the explicit credential-change variant of Regenerate.
// It may preserve a winning session when another stale request removed the old
// row after an independent authorization change committed.
func RegenerateOrCreate(ctx *httpx.Context) error {
	state, ok := stateFrom(ctx)
	if !ok {
		return fmt.Errorf("web: session middleware is not installed")
	}
	state.accessed = true
	return state.manager.RegenerateOrCreate(ctx.Request.Context(), state.current)
}

// Destroy removes the persisted session and stages an expired cookie in the
// buffered response. A destroyed session is not saved again at commit.
func Destroy(ctx *httpx.Context) error {
	state, ok := stateFrom(ctx)
	if !ok {
		return fmt.Errorf("web: session middleware is not installed")
	}
	state.accessed = true
	if err := state.manager.Destroy(ctx.Request.Context(), ctx.Response, state.current); err != nil {
		return err
	}
	state.destroyed = true
	return nil
}

// Invalidate removes a stale server-side session without emitting a deletion
// cookie. This prevents an out-of-order stale response from overwriting a new
// cookie issued by a concurrent credential-change request.
func Invalidate(ctx *httpx.Context) error {
	state, ok := stateFrom(ctx)
	if !ok {
		return fmt.Errorf("web: session middleware is not installed")
	}
	state.accessed = true
	if err := state.manager.Invalidate(ctx.Request.Context(), ctx.Response, state.current); err != nil {
		return err
	}
	state.destroyed = true
	return nil
}

// Renderer is implemented by view.Engine and by application-specific renderers.
type Renderer interface {
	Render(http.ResponseWriter, int, string, any) error
}

// Render renders through the current buffered browser response.
func Render(ctx *httpx.Context, renderer Renderer, status int, name string, data any) error {
	if renderer == nil {
		return fmt.Errorf("web: renderer is required")
	}
	return renderer.Render(ctx.Response, status, name, data)
}

// Redirect performs a Post/Redirect/Get redirect to an absolute application
// path using HTTP 303. External and scheme-relative locations are rejected.
func Redirect(ctx *httpx.Context, location string) error {
	parsed, err := url.ParseRequestURI(location)
	if err != nil || !strings.HasPrefix(location, "/") || strings.HasPrefix(location, "//") || strings.HasPrefix(location, `/\`) || parsed.IsAbs() || parsed.Host != "" {
		return fmt.Errorf("web: redirect location must be an absolute application path")
	}
	return ctx.Redirect(http.StatusSeeOther, location)
}

// ExternalRedirect is the explicit escape hatch for trusted external
// destinations. Never pass unvalidated request input to this helper.
func ExternalRedirect(ctx *httpx.Context, location string) error {
	parsed, err := url.ParseRequestURI(location)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("web: external redirect location must be an absolute HTTP(S) URL")
	}
	return ctx.Redirect(http.StatusSeeOther, location)
}

func stateFrom(ctx *httpx.Context) (*sessionState, bool) {
	if ctx == nil {
		return nil, false
	}
	value, ok := ctx.Get(sessionContextKey)
	if !ok {
		return nil, false
	}
	state, ok := value.(*sessionState)
	return state, ok
}

type responseBuffer struct {
	header      http.Header
	body        bytes.Buffer
	maxBytes    int64
	err         error
	status      int
	wroteHeader bool
}

func newResponseBuffer(initial http.Header, maxBytes int64) *responseBuffer {
	return &responseBuffer{header: initial.Clone(), maxBytes: maxBytes, status: http.StatusOK}
}

func (response *responseBuffer) Header() http.Header { return response.header }

func (response *responseBuffer) WriteHeader(status int) {
	if response.wroteHeader {
		return
	}
	response.status = status
	response.wroteHeader = true
}

func (response *responseBuffer) Write(body []byte) (int, error) {
	if response.err != nil {
		return 0, response.err
	}
	if int64(response.body.Len())+int64(len(body)) > response.maxBytes {
		response.err = ErrBufferedResponseTooLarge
		return 0, response.err
	}
	if !response.wroteHeader {
		response.WriteHeader(http.StatusOK)
	}
	return response.body.Write(body)
}

func (response *responseBuffer) commit(destination http.ResponseWriter) error {
	destinationHeader := destination.Header()
	for name := range destinationHeader {
		destinationHeader.Del(name)
	}
	for name, values := range response.header {
		destinationHeader[name] = append([]string(nil), values...)
	}
	destination.WriteHeader(response.status)
	if response.body.Len() == 0 {
		return nil
	}
	if _, err := destination.Write(response.body.Bytes()); err != nil {
		return err
	}
	return nil
}
