package errors

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/view"
)

type Page struct {
	Title   string
	Message string
}

// New returns the application's explicit browser/API error boundary. Browser
// routes render HTML; API routes retain GoForge's JSON envelope.
func New(renderer *view.Engine) httpx.ErrorHandler {
	return func(ctx *httpx.Context, err error) {
		status, message := safeError(err)
		if browserSurface(ctx) {
			if renderErr := renderer.Render(ctx.Response, status, "errors/error", Page{
				Title: http.StatusText(status), Message: message,
			}); renderErr == nil {
				return
			}
		}
		httpx.DefaultErrorHandler(ctx, err)
	}
}

func safeError(err error) (int, string) {
	status := http.StatusInternalServerError
	message := "internal server error"
	var httpError *httpx.HTTPError
	if errors.As(err, &httpError) && httpError.Status >= 400 && httpError.Status <= 599 {
		status = httpError.Status
		message = httpError.Message
	}
	return status, message
}

func browserSurface(ctx *httpx.Context) bool {
	name := ctx.RouteName()
	if name == "welcome" || strings.HasPrefix(name, "web.") {
		return true
	}
	// Global middleware may fail before route metadata is available.
	path := ctx.Request.URL.Path
	return path == "/register" || path == "/login" || path == "/logout" || path == "/app" || strings.HasPrefix(path, "/app/")
}
