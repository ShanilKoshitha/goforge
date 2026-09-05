package auth

import (
	"errors"
	"net/http"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/validation"
	"github.com/ShanilKoshitha/goforge/view"
	"github.com/ShanilKoshitha/goforge/web"
)

type AuthPage struct {
	Title string
	Form  view.Form
}

type DashboardPage struct {
	Title  string
	Form   view.Form
	Notice string
	User   User
}

type WebController struct {
	auth  *Controller
	views *view.Engine
}

func NewWebController(auth *Controller, views *view.Engine) *WebController {
	return &WebController{auth: auth, views: views}
}

func (controller *WebController) RegisterForm(ctx *httpx.Context) error {
	return controller.renderAuth(ctx, http.StatusOK, "auth/register", AuthPage{Title: "Create account"})
}

func (controller *WebController) Register(ctx *httpx.Context) error {
	var request RegisterRequest
	if err := request.DecodeForm(ctx); err != nil {
		return err
	}
	page := AuthPage{Title: "Create account", Form: view.Form{OldValues: map[string]string{"name": request.Name, "email": request.Email}}}
	if problems := request.Validate(); !problems.Empty() {
		page.Form.ValidationErrors = problems.Messages()
		return controller.renderAuth(ctx, http.StatusUnprocessableEntity, "auth/register", page)
	}
	user, err := controller.auth.createUserForRequest(ctx, request)
	if errors.Is(err, ErrEmailTaken) {
		page.Form.ValidationErrors = validation.Errors{"email": {ErrEmailTaken.Error()}}
		return controller.renderAuth(ctx, http.StatusUnprocessableEntity, "auth/register", page)
	}
	if limited, response := controller.renderRateLimit(ctx, err, "auth/register", "Create account"); limited {
		return response
	}
	if err != nil {
		return err
	}
	return controller.authenticated(ctx, user, "Welcome to GoForge.")
}

func (controller *WebController) LoginForm(ctx *httpx.Context) error {
	return controller.renderAuth(ctx, http.StatusOK, "auth/login", AuthPage{Title: "Sign in"})
}

func (controller *WebController) Login(ctx *httpx.Context) error {
	var request LoginRequest
	if err := request.DecodeForm(ctx); err != nil {
		return err
	}
	page := AuthPage{Title: "Sign in", Form: view.Form{OldValues: map[string]string{"email": request.Email}}}
	if problems := request.Validate(); !problems.Empty() {
		page.Form.ValidationErrors = problems.Messages()
		return controller.renderAuth(ctx, http.StatusUnprocessableEntity, "auth/login", page)
	}
	user, err := controller.auth.authenticateRequest(ctx, request)
	if errors.Is(err, ErrInvalidCredentials) {
		page.Form.ValidationErrors = validation.Errors{"email": {ErrInvalidCredentials.Error()}}
		return controller.renderAuth(ctx, http.StatusUnprocessableEntity, "auth/login", page)
	}
	if limited, response := controller.renderRateLimit(ctx, err, "auth/login", "Sign in"); limited {
		return response
	}
	if err != nil {
		return err
	}
	return controller.authenticated(ctx, user, "Welcome back.")
}

func (controller *WebController) Dashboard(ctx *httpx.Context) error {
	user, ok := UserFrom(ctx)
	if !ok {
		return httpx.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	current, ok := web.Session(ctx)
	if !ok {
		return errors.New("browser session is unavailable")
	}
	var notice string
	if _, err := current.Get("notice", &notice); err != nil {
		return err
	}
	token, err := web.CSRFToken(ctx)
	if err != nil {
		return err
	}
	return web.Render(ctx, controller.views, http.StatusOK, "pages/dashboard", DashboardPage{
		Title: "Dashboard", Form: view.Form{CSRFToken: token}, Notice: notice, User: user,
	})
}

func (controller *WebController) Logout(ctx *httpx.Context) error {
	if err := web.Destroy(ctx); err != nil {
		return err
	}
	return web.Redirect(ctx, "/login")
}

func (controller *WebController) renderAuth(ctx *httpx.Context, status int, name string, page AuthPage) error {
	token, err := web.CSRFToken(ctx)
	if err != nil {
		return err
	}
	page.Form.CSRFToken = token
	if page.Form.ValidationErrors == nil {
		page.Form.ValidationErrors = make(validation.Errors)
	}
	if page.Form.OldValues == nil {
		page.Form.OldValues = make(map[string]string)
	}
	return web.Render(ctx, controller.views, status, name, page)
}

func (controller *WebController) renderRateLimit(ctx *httpx.Context, err error, name, title string) (bool, error) {
	var httpError *httpx.HTTPError
	if !errors.As(err, &httpError) || httpError.Status != http.StatusTooManyRequests {
		return false, nil
	}
	return true, controller.renderAuth(ctx, http.StatusTooManyRequests, name, AuthPage{
		Title: title,
		Form: view.Form{ValidationErrors: validation.Errors{
			"email": {"Too many attempts. Try again later."},
		}},
	})
}

func (controller *WebController) authenticated(ctx *httpx.Context, user User, notice string) error {
	if err := web.Regenerate(ctx); err != nil {
		return err
	}
	current, ok := web.Session(ctx)
	if !ok {
		return errors.New("browser session is unavailable")
	}
	if err := current.Put("user_id", user.ID); err != nil {
		return err
	}
	if _, err := web.RotateCSRF(ctx); err != nil {
		return err
	}
	if err := current.Flash("notice", notice); err != nil {
		return err
	}
	return web.Redirect(ctx, "/app")
}
