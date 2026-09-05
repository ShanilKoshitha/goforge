package httpx

import "net/http"

// Adapt is the escape hatch from an http.Handler into a GoForge Handler.
func Adapt(handler http.Handler) Handler {
	return func(ctx *Context) error {
		handler.ServeHTTP(ctx.Response, ctx.Request)
		return nil
	}
}

// Standard converts a GoForge Handler to a standard-library http.Handler.
// Errors use DefaultErrorHandler; use Router.OnError for application policy.
func Standard(handler Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		ctx := newContext(response, request)
		if err := handler(ctx); err != nil && !ctx.response.Written() {
			DefaultErrorHandler(ctx, err)
		}
	})
}
