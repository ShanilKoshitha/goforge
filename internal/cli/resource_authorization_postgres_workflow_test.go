package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGeneratedResourceAuthorizationPostgresWorkflow(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}

	root := projectRoot(t)
	scratch := t.TempDir()
	forgeBinary := filepath.Join(scratch, "forge")
	if runtime.GOOS == "windows" {
		forgeBinary += ".exe"
	}
	baseEnvironment := append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := authorizationWorkflowCommand(root, baseEnvironment, "go", "build", "-o", forgeBinary, "./cmd/forge"); err != nil {
		t.Fatalf("build forge CLI: %v\n%s", err, output)
	}

	directory := filepath.Join(scratch, "authorization")
	if output, err := authorizationWorkflowCommand(scratch, baseEnvironment, forgeBinary, "new", "authorization", "--module", "example.com/authorization", "--replace", root); err != nil {
		t.Fatalf("forge new: %v\n%s", err, output)
	}
	if output, err := authorizationWorkflowCommand(directory, baseEnvironment, forgeBinary, "make:resource", "Issue", "--field", "title:string"); err != nil {
		t.Fatalf("forge make:resource Issue: %v\n%s", err, output)
	}
	customizeGeneratedIssueAuthorization(t, directory)

	proofPath := filepath.Join(directory, ".forge", "resource_authorization_acceptance.go")
	if err := os.WriteFile(proofPath, []byte(resourceAuthorizationAcceptanceProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := authorizationWorkflowCommand(directory, baseEnvironment, "go", "test", "./..."); err != nil {
		t.Fatalf("generated authorization application tests: %v\n%s", err, output)
	}
	if output, err := authorizationWorkflowCommand(directory, baseEnvironment, "go", "build", "./.forge/resource_authorization_acceptance.go"); err != nil {
		t.Fatalf("generated authorization acceptance helper build: %v\n%s", err, output)
	}
	if output, err := authorizationWorkflowCommand(directory, baseEnvironment, forgeBinary, "build"); err != nil {
		t.Fatalf("build generated authorization application: %v\n%s", err, output)
	}

	schema := fmt.Sprintf("goforge_resource_authorization_%d", time.Now().UnixNano())
	adminPath := filepath.Join(directory, ".forge", "acceptance_db.go")
	if err := os.WriteFile(adminPath, []byte(postgresAdminProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	adminEnvironment := append(baseEnvironment, "DATABASE_URL="+databaseURL)
	if output, err := authorizationWorkflowCommand(directory, adminEnvironment, "go", "run", "./.forge/acceptance_db.go", "create", schema); err != nil {
		t.Fatalf("create isolated PostgreSQL schema: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		if output, err := authorizationWorkflowCommand(directory, adminEnvironment, "go", "run", "./.forge/acceptance_db.go", "drop", schema); err != nil {
			t.Errorf("drop isolated PostgreSQL schema: %v\n%s", err, output)
		}
	})
	isolatedURL, err := postgresSchemaURL(databaseURL, schema)
	if err != nil {
		t.Fatal(err)
	}
	environment := append(baseEnvironment, "DATABASE_URL="+isolatedURL, "APP_ENV=local")
	if output, err := authorizationWorkflowCommand(directory, environment, forgeBinary, "migrate"); err != nil {
		t.Fatalf("migrate generated authorization application: %v\n%s", err, output)
	}

	address := freeAddress(t)
	serverEnvironment := append(environment, "APP_ADDRESS="+address)
	server, serverOutput := startGeneratedServer(t, filepath.Join(directory, workflowBuildDestination()), directory, serverEnvironment)
	defer func() {
		if server.ProcessState == nil {
			stopCommandProcess(t, server, false)
		}
	}()
	baseURL := "http://" + address
	waitForHealth(t, baseURL, serverOutput)

	guest := clientWithCookiesNoRedirect(t)
	response, body := requestJSON(t, guest, http.MethodGet, baseURL+"/issues", "")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("guest JSON list: got %d, want 401: %s", response.StatusCode, body)
	}
	response, body = requestBrowser(t, guest, http.MethodGet, baseURL+"/app/issues", nil)
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/login" {
		t.Fatalf("guest browser list: got %d location=%q, want 303 /login: %s", response.StatusCode, response.Header.Get("Location"), body)
	}

	ownerOneClient := clientWithCookiesNoRedirect(t)
	ownerTwoClient := clientWithCookiesNoRedirect(t)
	adminClient := clientWithCookiesNoRedirect(t)
	readonlyClient := clientWithCookiesNoRedirect(t)
	registerAuthorizationUser(t, ownerOneClient, baseURL, "Owner One", "owner-one@example.com")
	registerAuthorizationUser(t, ownerTwoClient, baseURL, "Owner Two", "owner-two@example.com")
	registerAuthorizationUser(t, adminClient, baseURL, "Administrator", "admin@example.com")
	registerAuthorizationUser(t, readonlyClient, baseURL, "Read Only", "readonly@example.com")

	ownerOne := createAuthorizationIssue(t, ownerOneClient, baseURL, "owner one")
	ownerTwo := createAuthorizationIssue(t, ownerTwoClient, baseURL, "owner two")
	readonly := createAuthorizationIssue(t, readonlyClient, baseURL, "readonly retained")
	missingID := int64(9_223_372_036_854_775_000)
	ownerTwoURL := fmt.Sprintf("%s/issues/%d", baseURL, ownerTwo.ID)
	missingURL := fmt.Sprintf("%s/issues/%d", baseURL, missingID)

	response, crossOwnerView := requestJSON(t, ownerOneClient, http.MethodGet, ownerTwoURL, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner JSON view: got %d, want 404: %s", response.StatusCode, crossOwnerView)
	}
	response, missingView := requestJSON(t, ownerOneClient, http.MethodGet, missingURL, "")
	if response.StatusCode != http.StatusNotFound || crossOwnerView != missingView {
		t.Fatalf("cross-owner and missing JSON views were distinguishable: status=%d cross-owner=%s missing=%s", response.StatusCode, crossOwnerView, missingView)
	}
	foreignUpdate := fmt.Sprintf(`{"title":"stolen","version":%d}`, ownerTwo.Version)
	response, crossOwnerUpdate := requestJSON(t, ownerOneClient, http.MethodPut, ownerTwoURL, foreignUpdate)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner JSON update: got %d, want 404: %s", response.StatusCode, crossOwnerUpdate)
	}
	response, missingUpdate := requestJSON(t, ownerOneClient, http.MethodPut, missingURL, foreignUpdate)
	if response.StatusCode != http.StatusNotFound || crossOwnerUpdate != missingUpdate {
		t.Fatalf("cross-owner and missing JSON updates were distinguishable: status=%d cross-owner=%s missing=%s", response.StatusCode, crossOwnerUpdate, missingUpdate)
	}
	response, crossOwnerDelete := requestJSON(t, ownerOneClient, http.MethodDelete, ownerTwoURL, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner JSON delete: got %d, want 404: %s", response.StatusCode, crossOwnerDelete)
	}
	response, missingDelete := requestJSON(t, ownerOneClient, http.MethodDelete, missingURL, "")
	if response.StatusCode != http.StatusNotFound || crossOwnerDelete != missingDelete {
		t.Fatalf("cross-owner and missing JSON deletes were distinguishable: status=%d cross-owner=%s missing=%s", response.StatusCode, crossOwnerDelete, missingDelete)
	}
	response, body = requestJSON(t, ownerTwoClient, http.MethodGet, ownerTwoURL, "")
	unchangedOwnerTwo := decodeAuthorizationIssue(t, body)
	if response.StatusCode != http.StatusOK || unchangedOwnerTwo.Title != "owner two" || unchangedOwnerTwo.Version != ownerTwo.Version {
		t.Fatalf("cross-owner mutations changed foreign row: status=%d issue=%#v body=%s", response.StatusCode, unchangedOwnerTwo, body)
	}
	response, crossOwnerBrowserView := requestBrowser(t, ownerOneClient, http.MethodGet, fmt.Sprintf("%s/app/issues/%d", baseURL, ownerTwo.ID), nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner browser view: got %d, want 404: %s", response.StatusCode, crossOwnerBrowserView)
	}
	response, missingBrowserView := requestBrowser(t, ownerOneClient, http.MethodGet, fmt.Sprintf("%s/app/issues/%d", baseURL, missingID), nil)
	if response.StatusCode != http.StatusNotFound || crossOwnerBrowserView != missingBrowserView {
		t.Fatalf("cross-owner and missing browser views were distinguishable: status=%d cross-owner=%s missing=%s", response.StatusCode, crossOwnerBrowserView, missingBrowserView)
	}

	response, body = requestJSON(t, adminClient, http.MethodGet, baseURL+"/issues", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("admin JSON list: got %d: %s", response.StatusCode, body)
	}
	listed := decodeAuthorizationIssueList(t, body)
	for _, expectedID := range []int64{ownerOne.ID, ownerTwo.ID, readonly.ID} {
		if !authorizationIssueListContains(listed, expectedID) {
			t.Fatalf("admin list omitted issue %d: %#v", expectedID, listed)
		}
	}
	ownerOneURL := fmt.Sprintf("%s/issues/%d", baseURL, ownerOne.ID)
	response, body = requestJSON(t, adminClient, http.MethodGet, ownerOneURL, "")
	adminViewed := decodeAuthorizationIssue(t, body)
	if response.StatusCode != http.StatusOK || adminViewed.UserID != ownerOne.UserID {
		t.Fatalf("admin JSON view of foreign row: status=%d issue=%#v body=%s", response.StatusCode, adminViewed, body)
	}
	response, body = requestJSON(t, adminClient, http.MethodPut, ownerOneURL,
		fmt.Sprintf(`{"title":"admin JSON updated","version":%d}`, ownerOne.Version))
	adminUpdated := decodeAuthorizationIssue(t, body)
	if response.StatusCode != http.StatusOK || adminUpdated.Title != "admin JSON updated" || adminUpdated.UserID != ownerOne.UserID || adminUpdated.Version != ownerOne.Version+1 {
		t.Fatalf("admin JSON update did not preserve owner: status=%d issue=%#v body=%s", response.StatusCode, adminUpdated, body)
	}

	ownerTwoBrowserPath := fmt.Sprintf("/app/issues/%d", ownerTwo.ID)
	response, body = requestBrowser(t, adminClient, http.MethodGet, baseURL+ownerTwoBrowserPath+"/edit", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `value="owner two"`) {
		t.Fatalf("admin browser edit of foreign row: got %d: %s", response.StatusCode, body)
	}
	adminCSRF := browserCSRF(t, body)
	adminVersion := browserVersion(t, body)
	response, body = requestBrowser(t, adminClient, http.MethodPost, baseURL+ownerTwoBrowserPath, url.Values{
		"_token": {adminCSRF}, "_method": {http.MethodPut}, "title": {"admin browser updated"}, "version": {strconv.FormatInt(adminVersion, 10)},
	})
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != ownerTwoBrowserPath {
		t.Fatalf("admin browser update of foreign row: got %d location=%q: %s", response.StatusCode, response.Header.Get("Location"), body)
	}
	response, body = requestBrowser(t, adminClient, http.MethodGet, baseURL+ownerTwoBrowserPath, nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "admin browser updated") {
		t.Fatalf("admin browser view of updated foreign row: got %d: %s", response.StatusCode, body)
	}

	readonlyURL := fmt.Sprintf("%s/issues/%d", baseURL, readonly.ID)
	response, body = requestJSON(t, readonlyClient, http.MethodDelete, readonlyURL, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("readonly JSON delete: got %d, want 403: %s", response.StatusCode, body)
	}
	readonlyBrowserPath := fmt.Sprintf("/app/issues/%d", readonly.ID)
	response, body = requestBrowser(t, readonlyClient, http.MethodGet, baseURL+readonlyBrowserPath, nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "readonly retained") {
		t.Fatalf("readonly browser view before denied delete: got %d: %s", response.StatusCode, body)
	}
	readonlyCSRF := browserCSRF(t, body)
	response, body = requestBrowser(t, readonlyClient, http.MethodPost, baseURL+readonlyBrowserPath, url.Values{
		"_token": {readonlyCSRF}, "_method": {http.MethodDelete},
	})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("readonly browser delete with valid CSRF: got %d, want 403: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, readonlyClient, http.MethodGet, readonlyURL, "")
	readonlyAfterDenials := decodeAuthorizationIssue(t, body)
	if response.StatusCode != http.StatusOK || readonlyAfterDenials.Title != readonly.Title || readonlyAfterDenials.Version != readonly.Version || readonlyAfterDenials.UserID != readonly.UserID {
		t.Fatalf("denied deletes changed readonly row: status=%d issue=%#v body=%s", response.StatusCode, readonlyAfterDenials, body)
	}

	response, body = requestJSON(t, adminClient, http.MethodDelete, ownerOneURL, "")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("admin JSON delete of foreign row: got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, adminClient, http.MethodGet, ownerOneURL, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("admin view after foreign delete: got %d, want 404: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, adminClient, http.MethodGet, missingURL, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("admin view of missing row: got %d, want 404: %s", response.StatusCode, body)
	}

	if output, err := authorizationWorkflowCommand(directory, environment, "go", "run", "./.forge/resource_authorization_acceptance.go",
		strconv.FormatInt(ownerOne.ID, 10), strconv.FormatInt(ownerTwo.ID, 10), strconv.FormatInt(readonly.ID, 10)); err != nil {
		t.Fatalf("real PostgreSQL authorization proof: %v\n%s", err, output)
	} else if strings.TrimSpace(output) != "resource authorization acceptance passed" {
		t.Fatalf("unexpected authorization proof output: %s", output)
	}
}

func authorizationWorkflowCommand(directory string, environment []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = environment
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return string(output), fmt.Errorf("%s timed out: %w", name, ctx.Err())
	}
	return string(output), err
}

func customizeGeneratedIssueAuthorization(t *testing.T, directory string) {
	t.Helper()
	path := filepath.Join(directory, "internal", "resources", "issue", "authorization.go")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	start := strings.Index(source, "func Authorize(")
	if start < 0 || strings.Count(source, "func Authorize(") != 1 {
		t.Fatalf("generated Issue authorization policy had an unexpected shape: %s", source)
	}
	endMarker := "\n}\n\n// Scope"
	end := strings.Index(source[start:], endMarker)
	if end < 0 {
		t.Fatalf("generated Issue authorization policy had an unexpected shape: %s", source)
	}
	end += start + len("\n}")
	custom := `func Authorize(_ context.Context, user auth.User, action Action) (Access, error) {
	switch action {
	case ActionList, ActionCreate, ActionView, ActionUpdate, ActionDelete:
		if user.Email == "admin@example.com" {
			return AccessAll, nil
		}
		if user.Email == "readonly@example.com" && action == ActionDelete {
			return AccessDenied, nil
		}
		return AccessOwner, nil
	default:
		return AccessDenied, nil
	}
}`
	source = source[:start] + custom + source[end:]
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

type authorizationIssue struct {
	ID      int64  `json:"id"`
	UserID  int64  `json:"user_id"`
	Title   string `json:"title"`
	Version int64  `json:"version"`
}

func registerAuthorizationUser(t *testing.T, client *http.Client, baseURL, name, email string) {
	t.Helper()
	response, body := requestJSON(t, client, http.MethodPost, baseURL+"/auth/register",
		fmt.Sprintf(`{"name":%q,"email":%q,"password":"a secure passphrase","password_confirmation":"a secure passphrase"}`, name, email))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register authorization user %s: got %d: %s", email, response.StatusCode, body)
	}
}

func createAuthorizationIssue(t *testing.T, client *http.Client, baseURL, title string) authorizationIssue {
	t.Helper()
	response, body := requestJSON(t, client, http.MethodPost, baseURL+"/issues", fmt.Sprintf(`{"title":%q}`, title))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create authorization issue %q: got %d: %s", title, response.StatusCode, body)
	}
	issue := decodeAuthorizationIssue(t, body)
	if issue.ID < 1 || issue.UserID < 1 || issue.Version != 1 || issue.Title != title {
		t.Fatalf("created authorization issue = %#v, want title=%q with IDs and version 1", issue, title)
	}
	return issue
}

func decodeAuthorizationIssue(t *testing.T, body string) authorizationIssue {
	t.Helper()
	var envelope struct {
		Data authorizationIssue `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode authorization issue: %v: %s", err, body)
	}
	return envelope.Data
}

func decodeAuthorizationIssueList(t *testing.T, body string) []authorizationIssue {
	t.Helper()
	var envelope struct {
		Data []authorizationIssue `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode authorization issue list: %v: %s", err, body)
	}
	return envelope.Data
}

func authorizationIssueListContains(issues []authorizationIssue, id int64) bool {
	for _, issue := range issues {
		if issue.ID == id {
			return true
		}
	}
	return false
}

const resourceAuthorizationAcceptanceProgram = `package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	if len(os.Args) != 4 {
		panic("usage: resource_authorization_acceptance <deleted-id> <browser-updated-id> <readonly-id>")
	}
	ids := make([]int64, 3)
	for index, value := range os.Args[1:] {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 1 {
			panic(fmt.Sprintf("invalid issue ID %q", value))
		}
		ids[index] = parsed
	}
	database, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		panic(err)
	}
	var deletedCount int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM issues WHERE id = $1", ids[0]).Scan(&deletedCount); err != nil {
		panic(err)
	}
	if deletedCount != 0 {
		panic(fmt.Sprintf("admin-deleted issue count = %d, want 0", deletedCount))
	}
	want := []struct {
		id    int64
		email string
		title string
	}{
		{ids[1], "owner-two@example.com", "admin browser updated"},
		{ids[2], "readonly@example.com", "readonly retained"},
	}
	for _, expected := range want {
		var issueUserID, joinedUserID int64
		var email, title string
		err := database.QueryRowContext(ctx, "SELECT issues.user_id, users.id, users.email, issues.title FROM issues JOIN users ON users.id = issues.user_id WHERE issues.id = $1", expected.id).
			Scan(&issueUserID, &joinedUserID, &email, &title)
		if err != nil {
			panic(err)
		}
		if issueUserID != joinedUserID || email != expected.email || title != expected.title {
			panic(fmt.Sprintf("issue %d state = user_id:%d joined_user_id:%d email:%q title:%q", expected.id, issueUserID, joinedUserID, email, title))
		}
	}
	var issueCount int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM issues").Scan(&issueCount); err != nil {
		panic(err)
	}
	if issueCount != 2 {
		panic(fmt.Sprintf("final issue count = %d, want 2", issueCount))
	}
	fmt.Println("resource authorization acceptance passed")
}
`
