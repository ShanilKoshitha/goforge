package issue

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/validation"
	"github.com/ShanilKoshitha/goforge/view"
	"github.com/ShanilKoshitha/goforge/web"

	"example.com/format6/internal/auth"
)

const webBasePath = "/app/issues"

type IndexPage struct {
	Title       string
	Form        view.Form
	Notice      string
	Items       []Issue
	Page        int
	PerPage     int
	PreviousURL string
	NextURL     string
}

type ShowPage struct {
	Title  string
	Form   view.Form
	Notice string
	Item   Issue
}

type FormPage struct {
	Title   string
	Form    view.Form
	Action  string
	Method  string
	Submit  string
	Name    string
	Version *int64
	Errors  validation.Errors
}

type WebController struct {
	repository Repository
	views      *view.Engine
}

func NewWebController(repository Repository, views *view.Engine) *WebController {
	return &WebController{repository: repository, views: views}
}

func (controller *WebController) Index(ctx *httpx.Context) error {
	user, err := webUser(ctx)
	if err != nil {
		return err
	}
	pageNumber, pageSize, err := paginationParameters(ctx)
	if err != nil {
		return err
	}
	page, err := controller.repository.Paginate(ctx.Request.Context(), user.ID, pageNumber, pageSize)
	if err != nil {
		return err
	}
	token, notice, err := browserState(ctx)
	if err != nil {
		return err
	}
	return web.Render(ctx, controller.views, http.StatusOK, "pages/issues/index", IndexPage{
		Title: "Issues", Form: view.Form{CSRFToken: token}, Notice: notice, Items: page.Items,
		Page: page.Number, PerPage: page.PerPage,
		PreviousURL: paginationURL(page.Number-1, page.PerPage, page.HasPrevious),
		NextURL:     paginationURL(page.Number+1, page.PerPage, page.HasNext),
	})
}

func (controller *WebController) New(ctx *httpx.Context) error {
	if _, err := webUser(ctx); err != nil {
		return err
	}
	return controller.renderForm(ctx, http.StatusOK, "pages/issues/new", FormPage{
		Title: "New Issue", Action: webBasePath, Submit: "Create Issue",
	})
}

func (controller *WebController) Create(ctx *httpx.Context) error {
	user, err := webUser(ctx)
	if err != nil {
		return err
	}
	var request WriteRequest
	if err := request.DecodeForm(ctx); err != nil {
		return err
	}
	if problems := request.Validate(); !problems.Empty() {
		return controller.renderForm(ctx, http.StatusUnprocessableEntity, "pages/issues/new", FormPage{
			Title: "New Issue", Action: webBasePath, Submit: "Create Issue", Name: request.Name, Errors: problems.Messages(),
		})
	}
	item, err := controller.repository.Create(ctx.Request.Context(), user.ID, request.Name)
	if err != nil {
		return err
	}
	if err := flashNotice(ctx, "Issue created."); err != nil {
		return err
	}
	return web.Redirect(ctx, webItemPath(item.ID))
}

func (controller *WebController) Show(ctx *httpx.Context) error {
	user, err := webUser(ctx)
	if err != nil {
		return err
	}
	id, err := resourceID(ctx)
	if err != nil {
		return err
	}
	item, err := controller.repository.Find(ctx.Request.Context(), user.ID, id)
	if errors.Is(err, ErrNotFound) {
		return httpx.NewHTTPError(http.StatusNotFound, "issue not found")
	}
	if err != nil {
		return err
	}
	token, notice, err := browserState(ctx)
	if err != nil {
		return err
	}
	return web.Render(ctx, controller.views, http.StatusOK, "pages/issues/show", ShowPage{
		Title: item.Name, Form: view.Form{CSRFToken: token}, Notice: notice, Item: item,
	})
}

func (controller *WebController) Edit(ctx *httpx.Context) error {
	user, err := webUser(ctx)
	if err != nil {
		return err
	}
	id, err := resourceID(ctx)
	if err != nil {
		return err
	}
	item, err := controller.repository.Find(ctx.Request.Context(), user.ID, id)
	if errors.Is(err, ErrNotFound) {
		return httpx.NewHTTPError(http.StatusNotFound, "issue not found")
	}
	if err != nil {
		return err
	}
	return controller.renderForm(ctx, http.StatusOK, "pages/issues/edit", FormPage{
		Title: "Edit Issue", Action: webItemPath(id), Method: http.MethodPut,
		Submit: "Save Issue", Name: item.Name, Version: &item.Version,
	})
}

func (controller *WebController) Update(ctx *httpx.Context) error {
	user, err := webUser(ctx)
	if err != nil {
		return err
	}
	id, err := resourceID(ctx)
	if err != nil {
		return err
	}
	var request WriteRequest
	if err := request.DecodeForm(ctx); err != nil {
		return err
	}
	if problems := request.Validate(); !problems.Empty() {
		return controller.renderForm(ctx, http.StatusUnprocessableEntity, "pages/issues/edit", FormPage{
			Title: "Edit Issue", Action: webItemPath(id), Method: http.MethodPut,
			Submit: "Save Issue", Name: request.Name, Version: request.Version, Errors: problems.Messages(),
		})
	}
	if request.Version == nil {
		return httpx.NewHTTPError(http.StatusBadRequest, "version is required for browser updates")
	}
	item, err := controller.repository.Update(ctx.Request.Context(), user.ID, id, request.Name, request.Version)
	if errors.Is(err, ErrNotFound) {
		return httpx.NewHTTPError(http.StatusNotFound, "issue not found")
	}
	if errors.Is(err, ErrStale) {
		fresh, findErr := controller.repository.Find(ctx.Request.Context(), user.ID, id)
		if errors.Is(findErr, ErrNotFound) {
			return httpx.NewHTTPError(http.StatusNotFound, "issue not found")
		}
		if findErr != nil {
			return findErr
		}
		return controller.renderForm(ctx, http.StatusConflict, "pages/issues/edit", FormPage{
			Title: "Edit Issue", Action: webItemPath(id), Method: http.MethodPut,
			Submit: "Save Issue", Name: request.Name, Version: &fresh.Version,
			Errors: validation.Errors{"version": {"was changed by another request; review and try again"}},
		})
	}
	if err != nil {
		return err
	}
	if err := flashNotice(ctx, "Issue updated."); err != nil {
		return err
	}
	return web.Redirect(ctx, webItemPath(item.ID))
}

func (controller *WebController) Delete(ctx *httpx.Context) error {
	user, err := webUser(ctx)
	if err != nil {
		return err
	}
	id, err := resourceID(ctx)
	if err != nil {
		return err
	}
	if err := controller.repository.Delete(ctx.Request.Context(), user.ID, id); errors.Is(err, ErrNotFound) {
		return httpx.NewHTTPError(http.StatusNotFound, "issue not found")
	} else if err != nil {
		return err
	}
	if err := flashNotice(ctx, "Issue deleted."); err != nil {
		return err
	}
	return web.Redirect(ctx, webBasePath)
}

func (controller *WebController) renderForm(ctx *httpx.Context, status int, name string, page FormPage) error {
	token, err := web.CSRFToken(ctx)
	if err != nil {
		return err
	}
	page.Form.CSRFToken = token
	if page.Errors == nil {
		page.Errors = make(validation.Errors)
	}
	page.Form.ValidationErrors = page.Errors
	if !page.Errors.Empty() {
		page.Form.OldValues = map[string]string{"name": page.Name}
	}
	return web.Render(ctx, controller.views, status, name, page)
}

func webUser(ctx *httpx.Context) (auth.User, error) {
	user, ok := auth.UserFrom(ctx)
	if !ok {
		return auth.User{}, httpx.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	return user, nil
}

func browserState(ctx *httpx.Context) (string, string, error) {
	token, err := web.CSRFToken(ctx)
	if err != nil {
		return "", "", err
	}
	current, ok := web.Session(ctx)
	if !ok {
		return "", "", errors.New("browser session is unavailable")
	}
	var notice string
	if _, err := current.Get("notice", &notice); err != nil {
		return "", "", err
	}
	return token, notice, nil
}

func flashNotice(ctx *httpx.Context, notice string) error {
	current, ok := web.Session(ctx)
	if !ok {
		return errors.New("browser session is unavailable")
	}
	return current.Flash("notice", notice)
}

func webItemPath(id int64) string {
	return webBasePath + "/" + strconv.FormatInt(id, 10)
}

func paginationParameters(ctx *httpx.Context) (int, int, error) {
	page, err := positiveQueryInteger(ctx, "page", defaultPageNumber)
	if err != nil {
		return 0, 0, err
	}
	perPage, err := positiveQueryInteger(ctx, "per_page", defaultPageSize)
	if err != nil {
		return 0, 0, err
	}
	page, perPage = normalizePagination(page, perPage)
	return page, perPage, nil
}

func positiveQueryInteger(ctx *httpx.Context, name string, fallback int) (int, error) {
	values, present := ctx.Request.URL.Query()[name]
	if !present {
		return fallback, nil
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return 0, httpx.NewHTTPError(http.StatusBadRequest, name+" must be one positive integer")
	}
	value, err := strconv.Atoi(values[0])
	if err != nil || value < 1 {
		return 0, httpx.NewHTTPError(http.StatusBadRequest, name+" must be one positive integer")
	}
	return value, nil
}

func paginationURL(page, perPage int, enabled bool) string {
	if !enabled {
		return ""
	}
	query := make(url.Values)
	query.Set("page", strconv.Itoa(page))
	query.Set("per_page", strconv.Itoa(perPage))
	return webBasePath + "?" + query.Encode()
}
