// Package httpx provides error-returning handlers and explicit middleware around
// the standard library HTTP server and router.
package httpx

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

type Handler func(*Context) error
type Middleware func(Handler) Handler

type Route struct {
	Method  string
	Path    string
	Name    string
	Handler Handler
}

type Router struct {
	mux          *http.ServeMux
	middleware   []Middleware
	routes       []Route
	named        map[string]string
	errorHandler ErrorHandler
	mu           sync.RWMutex
}

type contextKey struct{}

func NewRouter() *Router {
	return &Router{mux: http.NewServeMux(), named: make(map[string]string), errorHandler: DefaultErrorHandler}
}

func (router *Router) Use(middleware ...Middleware) {
	router.mu.Lock()
	defer router.mu.Unlock()
	router.middleware = append(router.middleware, middleware...)
}

func (router *Router) OnError(handler ErrorHandler) {
	if handler == nil {
		panic("httpx: error handler cannot be nil")
	}
	router.mu.Lock()
	defer router.mu.Unlock()
	router.errorHandler = handler
}

func (router *Router) Handle(method, path string, handler Handler) {
	router.Named("", method, path, handler)
}

func (router *Router) Named(name, method, path string, handler Handler) {
	if handler == nil {
		panic("httpx: route handler cannot be nil")
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" || !strings.HasPrefix(path, "/") {
		panic(fmt.Sprintf("httpx: invalid route %q %q", method, path))
	}

	router.mu.Lock()
	defer router.mu.Unlock()
	if name != "" {
		if _, exists := router.named[name]; exists {
			panic(fmt.Sprintf("httpx: duplicate route name %q", name))
		}
	}

	// ServeMux validates patterns and detects conflicts before metadata is added.
	router.mux.Handle(method+" "+path, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		ctx, ok := request.Context().Value(contextKey{}).(*Context)
		if !ok {
			panic("httpx: router invoked outside ServeHTTP")
		}
		ctx.routeName = name
		ctx.routePattern = path
		ctx.routeErr = handler(ctx)
	}))
	if name != "" {
		router.named[name] = path
	}
	router.routes = append(router.routes, Route{Method: method, Path: path, Name: name, Handler: handler})
}

var pathParameter = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)(\.\.\.)?\}`)

// URL builds a path for a named route. It fails when a required path parameter
// is absent; callers add query parameters with net/url as usual.
func (router *Router) URL(name string, parameters map[string]string) (string, error) {
	router.mu.RLock()
	path, ok := router.named[name]
	router.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("httpx: named route %q does not exist", name)
	}
	path = strings.TrimSuffix(path, "{$}")
	var missing string
	result := pathParameter.ReplaceAllStringFunc(path, func(match string) string {
		parts := pathParameter.FindStringSubmatch(match)
		value, exists := parameters[parts[1]]
		if !exists {
			missing = parts[1]
			return match
		}
		if parts[2] == "..." {
			segments := strings.Split(value, "/")
			for index := range segments {
				segments[index] = url.PathEscape(segments[index])
			}
			return strings.Join(segments, "/")
		}
		return url.PathEscape(value)
	})
	if missing != "" {
		return "", fmt.Errorf("httpx: route %q requires parameter %q", name, missing)
	}
	return result, nil
}

func (router *Router) GET(path string, handler Handler) { router.Handle(http.MethodGet, path, handler) }
func (router *Router) POST(path string, handler Handler) {
	router.Handle(http.MethodPost, path, handler)
}
func (router *Router) PUT(path string, handler Handler) { router.Handle(http.MethodPut, path, handler) }
func (router *Router) PATCH(path string, handler Handler) {
	router.Handle(http.MethodPatch, path, handler)
}
func (router *Router) DELETE(path string, handler Handler) {
	router.Handle(http.MethodDelete, path, handler)
}

func (router *Router) Routes() []Route {
	router.mu.RLock()
	defer router.mu.RUnlock()
	result := make([]Route, len(router.routes))
	copy(result, router.routes)
	return result
}

func (router *Router) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	ctx := newContext(response, request)
	request = request.WithContext(context.WithValue(request.Context(), contextKey{}, ctx))
	ctx.Request = request

	router.mu.RLock()
	middleware := append([]Middleware(nil), router.middleware...)
	errorHandler := router.errorHandler
	router.mu.RUnlock()

	handler := Handler(func(ctx *Context) error {
		router.mux.ServeHTTP(ctx.Response, ctx.Request)
		return ctx.routeErr
	})
	for index := len(middleware) - 1; index >= 0; index-- {
		handler = middleware[index](handler)
	}
	if err := handler(ctx); err != nil {
		if ctx.response.Written() {
			return
		}
		errorHandler(ctx, err)
	}
}

type Group struct {
	router     *Router
	prefix     string
	middleware []Middleware
}

func (router *Router) Group(prefix string, middleware ...Middleware) *Group {
	return &Group{router: router, prefix: strings.TrimSuffix(prefix, "/"), middleware: middleware}
}

func (group *Group) Handle(method, path string, handler Handler) {
	group.Named("", method, path, handler)
}

func (group *Group) Named(name, method, path string, handler Handler) {
	wrapped := handler
	for index := len(group.middleware) - 1; index >= 0; index-- {
		wrapped = group.middleware[index](wrapped)
	}
	group.router.Named(name, method, group.prefix+path, wrapped)
}

func (group *Group) GET(path string, handler Handler)  { group.Handle(http.MethodGet, path, handler) }
func (group *Group) POST(path string, handler Handler) { group.Handle(http.MethodPost, path, handler) }
func (group *Group) PUT(path string, handler Handler)  { group.Handle(http.MethodPut, path, handler) }
func (group *Group) PATCH(path string, handler Handler) {
	group.Handle(http.MethodPatch, path, handler)
}
func (group *Group) DELETE(path string, handler Handler) {
	group.Handle(http.MethodDelete, path, handler)
}
