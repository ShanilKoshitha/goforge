package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGeneratedPostgresWorkflow(t *testing.T) {
	started := time.Now()
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
	if output, err := generatedCommand(root, baseEnvironment, "go", "build", "-o", forgeBinary, "./cmd/forge"); err != nil {
		t.Fatalf("build forge CLI: %v\n%s", err, output)
	}
	directory := filepath.Join(scratch, "issueboard")
	if output, err := generatedCommand(scratch, baseEnvironment, forgeBinary, "new", "issueboard", "--module", "example.com/issueboard", "--replace", root); err != nil {
		t.Fatalf("forge new: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "make:component", "Notice"); err != nil {
		t.Fatalf("forge make:component: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "make:resource", "Issue"); err != nil {
		t.Fatalf("forge make:resource: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "views:compile", "--check"); err != nil {
		t.Fatalf("forge views:compile --check: %v\n%s", err, output)
	}
	compiledViews, err := os.ReadFile(filepath.Join(directory, "resources", "views", "views_gen.go"))
	if err != nil {
		t.Fatalf("read generated views: %v", err)
	}
	for _, expected := range []string{"Mappings:", `Name: "pages/issues/index"`, "headline", "{{if", ".Form.Errors"} {
		if !bytes.Contains(compiledViews, []byte(expected)) {
			t.Fatalf("generated view artifact omits %q:\n%s", expected, compiledViews)
		}
	}
	for _, directive := range []string{"@component(", "@slot(", "@if(", "@for(", "@csrf", "@push(", "@stack("} {
		if bytes.Contains(compiledViews, []byte(directive)) {
			t.Fatalf("generated view artifact retains Forge directive %q:\n%s", directive, compiledViews)
		}
	}
	componentPath := filepath.Join(directory, "resources", "views", "components", "card.forge.html")
	originalComponent, err := os.ReadFile(componentPath)
	if err != nil {
		t.Fatalf("read generated component: %v", err)
	}
	invalidComponent := "@props(title)\n@if(.Ready)\n@endfor\n"
	if err := os.WriteFile(componentPath, []byte(invalidComponent), 0o644); err != nil {
		t.Fatalf("write invalid component probe: %v", err)
	}
	output, compileErr := generatedCommand(directory, baseEnvironment, forgeBinary, "views:compile", "--check")
	if compileErr == nil || !strings.Contains(output, "components/card.forge.html:3:1") {
		t.Fatalf("composed view diagnostic = %v, want component source position:\n%s", compileErr, output)
	}
	afterInvalid, err := os.ReadFile(filepath.Join(directory, "resources", "views", "views_gen.go"))
	if err != nil {
		t.Fatalf("read last-good views after diagnostic: %v", err)
	}
	if !bytes.Equal(compiledViews, afterInvalid) {
		t.Fatal("invalid view check changed the last-good generated artifact")
	}
	if err := os.WriteFile(componentPath, originalComponent, 0o644); err != nil {
		t.Fatalf("restore generated component: %v", err)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "views:compile", "--check"); err != nil {
		t.Fatalf("restored views are not current: %v\n%s", err, output)
	}
	if err := writeRelationshipAcceptanceFixture(directory, "example.com/issueboard"); err != nil {
		t.Fatalf("write relationship acceptance fixture: %v", err)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "orm:generate"); err != nil {
		t.Fatalf("forge orm:generate relationship descriptors: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "orm:generate", "--check"); err != nil {
		t.Fatalf("forge orm:generate --check: %v\n%s", err, output)
	}
	for _, command := range [][]string{{"test", "./..."}, {"vet", "./..."}, {"build", "./cmd/..."}} {
		if output, err := generatedCommand(directory, baseEnvironment, "go", command...); err != nil {
			t.Fatalf("fresh application go %s: %v\n%s", strings.Join(command, " "), err, output)
		}
	}
	if output, err := generatedCommand(directory, baseEnvironment, "go", "build", "./.forge/relationship_acceptance.go"); err != nil {
		t.Fatalf("fresh application relationship acceptance helper build: %v\n%s", err, output)
	}
	t.Log("forge new, make:component, make:resource, view/ORM checks, tests, vet, and builds passed")
	schema := fmt.Sprintf("goforge_acceptance_%d", time.Now().UnixNano())
	adminPath := filepath.Join(directory, ".forge", "acceptance_db.go")
	if err := os.WriteFile(adminPath, []byte(postgresAdminProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	adminEnvironment := append(baseEnvironment, "DATABASE_URL="+databaseURL)
	if output, err := generatedCommand(directory, adminEnvironment, "go", "run", "./.forge/acceptance_db.go", "create", schema); err != nil {
		t.Fatalf("create isolated PostgreSQL schema: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		if output, err := generatedCommand(directory, adminEnvironment, "go", "run", "./.forge/acceptance_db.go", "drop", schema); err != nil {
			t.Errorf("drop isolated PostgreSQL schema: %v\n%s", err, output)
		}
	})
	isolatedURL, err := postgresSchemaURL(databaseURL, schema)
	if err != nil {
		t.Fatal(err)
	}
	environment := append(baseEnvironment,
		"DATABASE_URL="+isolatedURL,
		"APP_ENV=local",
	)

	// Two application processes migrate the same empty database concurrently.
	// PostgreSQL's advisory lock must let exactly one apply each migration.
	type commandResult struct {
		output string
		err    error
	}
	results := make(chan commandResult, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			command := exec.Command(forgeBinary, "migrate")
			command.Dir = directory
			command.Env = environment
			output, err := command.CombinedOutput()
			results <- commandResult{output: string(output), err: err}
		}()
	}
	group.Wait()
	close(results)
	combined := ""
	for result := range results {
		combined += result.output
		if result.err != nil {
			t.Fatalf("concurrent migrate failed: %v\n%s", result.err, result.output)
		}
	}
	if got := strings.Count(combined, "migrated "); got != 4 {
		t.Fatalf("expected three migrations to be applied exactly once, got %d:\n%s", got, combined)
	}
	if output, err := generatedCommand(directory, environment, forgeBinary, "migrate"); err != nil || !strings.Contains(output, "No pending migrations") {
		t.Fatalf("idempotent migrate failed: %v\n%s", err, output)
	}
	t.Log("concurrent and idempotent forge migrate passed")
	if output, err := generatedCommand(directory, environment, "go", "run", "./.forge/relationship_acceptance.go"); err != nil {
		t.Fatalf("generated relationship PostgreSQL acceptance: %v\n%s", err, output)
	} else if !strings.Contains(output, "relationship acceptance passed") {
		t.Fatalf("generated relationship acceptance did not report success:\n%s", output)
	}
	t.Log("generated relationship loaders, scopes, budgets, attach, and detach passed")

	// PostgreSQL transactional DDL must leave neither schema nor bookkeeping
	// after a failed migration: replacing the same version with valid SQL works.
	badUp := filepath.Join(directory, "database", "migrations", "99999999999999_atomic_probe.up.sql")
	badDown := filepath.Join(directory, "database", "migrations", "99999999999999_atomic_probe.down.sql")
	if err := os.WriteFile(badUp, []byte("CREATE TABLE goforge_atomic_probe (id BIGINT PRIMARY KEY); INVALID SQL;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badDown, []byte("DROP TABLE IF EXISTS goforge_atomic_probe;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := generatedCommand(directory, environment, forgeBinary, "migrate"); err == nil {
		t.Fatalf("invalid migration unexpectedly passed:\n%s", output)
	}
	if err := os.WriteFile(badUp, []byte("CREATE TABLE goforge_atomic_probe (id BIGINT PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := generatedCommand(directory, environment, forgeBinary, "migrate"); err != nil || !strings.Contains(output, "atomic_probe") {
		t.Fatalf("failed migration left partial state: %v\n%s", err, output)
	}
	t.Log("transactional migration recovery passed")
	if output, err := generatedCommand(directory, environment, "go", "run", "./.forge/acceptance_db.go", "seed-expired-session"); err != nil {
		t.Fatalf("seed abandoned expired session: %v\n%s", err, output)
	}

	address := freeAddress(t)
	serverEnvironment := append(environment, "APP_ADDRESS="+address)
	server := exec.Command(forgeBinary, "serve")
	server.Dir = directory
	server.Env = serverEnvironment
	configureCommandProcess(server)
	var serverOutput synchronizedBuffer
	server.Stdout, server.Stderr = &serverOutput, &serverOutput
	if err := server.Start(); err != nil {
		t.Fatalf("forge serve: %v", err)
	}
	serverRunning := true
	t.Cleanup(func() {
		if serverRunning {
			stopCommandProcess(t, server, false)
		}
	})
	baseURL := "http://" + address
	waitForHealth(t, baseURL, &serverOutput)
	t.Log("forge serve became healthy")

	plainClient := &http.Client{Timeout: 3 * time.Second}
	response, body := requestJSON(t, plainClient, http.MethodGet, baseURL+"/issues", "")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated issues: expected 401, got %d: %s", response.StatusCode, body)
	}

	first := clientWithCookies(t)
	stamp := time.Now().UnixNano()
	response, body = requestJSON(t, first, http.MethodPost, baseURL+"/auth/register",
		fmt.Sprintf(`{"name":"Ada","email":"ada-%d@example.com","password":"a secure passphrase","password_confirmation":"a secure passphrase"}`, stamp))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register first user: expected 201, got %d: %s", response.StatusCode, body)
	}
	if output, err := generatedCommand(directory, environment, "go", "run", "./.forge/acceptance_db.go", "count-expired-sessions"); err != nil || strings.TrimSpace(output) != "0" {
		t.Fatalf("bounded opportunistic session pruning failed: %v\n%s", err, output)
	}
	response, body = requestJSON(t, first, http.MethodPost, baseURL+"/issues", `{"name":"First issue"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create issue: expected 201, got %d: %s", response.StatusCode, body)
	}
	var created struct {
		Data struct {
			ID      int64 `json:"id"`
			Version int64 `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.Data.ID == 0 || created.Data.Version != 1 {
		t.Fatalf("decode created issue: %v: %s", err, body)
	}
	issueURL := fmt.Sprintf("%s/issues/%d", baseURL, created.Data.ID)
	response, body = requestJSON(t, first, http.MethodGet, baseURL+"/issues", "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "First issue") {
		t.Fatalf("list issues: expected created issue, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, first, http.MethodGet, issueURL, "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "First issue") {
		t.Fatalf("show owned issue: expected 200, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, first, http.MethodPut, issueURL,
		fmt.Sprintf(`{"name":"Guarded issue","version":%d}`, created.Data.Version))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("guarded update issue: expected 200, got %d: %s", response.StatusCode, body)
	}
	var guarded struct {
		Data struct {
			Name    string `json:"name"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &guarded); err != nil || guarded.Data.Name != "Guarded issue" || guarded.Data.Version != created.Data.Version+1 {
		t.Fatalf("guarded update did not increment exactly once: %v: %s", err, body)
	}
	response, body = requestJSON(t, first, http.MethodPut, issueURL,
		fmt.Sprintf(`{"name":"Stale overwrite","version":%d}`, created.Data.Version))
	if response.StatusCode != http.StatusConflict || !strings.Contains(body, "was changed") {
		t.Fatalf("stale guarded update: expected useful 409, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, first, http.MethodGet, issueURL, "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Guarded issue") || strings.Contains(body, "Stale overwrite") || !strings.Contains(body, fmt.Sprintf(`"version":%d`, guarded.Data.Version)) {
		t.Fatalf("stale update changed the row: got %d: %s", response.StatusCode, body)
	}

	duplicate := clientWithCookies(t)
	response, body = requestJSON(t, duplicate, http.MethodPost, baseURL+"/auth/register",
		fmt.Sprintf(`{"name":"Duplicate","email":"ada-%d@example.com","password":"a secure passphrase","password_confirmation":"a secure passphrase"}`, stamp))
	if response.StatusCode != http.StatusConflict || !strings.Contains(body, "email already registered") {
		t.Fatalf("duplicate registration: expected useful 409, got %d: %s", response.StatusCode, body)
	}

	second := clientWithCookies(t)
	response, body = requestJSON(t, second, http.MethodPost, baseURL+"/auth/register",
		fmt.Sprintf(`{"name":"Grace","email":"grace-%d@example.com","password":"another secure passphrase","password_confirmation":"another secure passphrase"}`, stamp))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register second user: expected 201, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, second, http.MethodGet, issueURL, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner read: expected 404, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, second, http.MethodPut, issueURL,
		fmt.Sprintf(`{"name":"Stolen issue","version":%d}`, guarded.Data.Version))
	if response.StatusCode != http.StatusNotFound || strings.Contains(body, "was changed") {
		t.Fatalf("cross-owner guarded update: expected 404 without stale disclosure, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, second, http.MethodDelete, issueURL, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner delete: expected 404, got %d: %s", response.StatusCode, body)
	}

	response, body = requestJSON(t, first, http.MethodPut, issueURL, `{"name":"Updated issue"}`)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Updated issue") || !strings.Contains(body, fmt.Sprintf(`"version":%d`, guarded.Data.Version+1)) {
		t.Fatalf("update issue: expected 200, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, first, http.MethodPost, baseURL+"/issues", `{"name":"x"}`)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid issue: expected 422, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, first, http.MethodDelete, issueURL, "")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete issue: expected 204, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, first, http.MethodPost, baseURL+"/auth/logout", `{}`)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: expected 204, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, first, http.MethodGet, baseURL+"/auth/me", "")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session survived logout: expected 401, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, first, http.MethodPost, baseURL+"/auth/login",
		fmt.Sprintf(`{"email":"ada-%d@example.com","password":"a secure passphrase"}`, stamp))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login after logout: expected 200, got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, first, http.MethodGet, baseURL+"/auth/me", "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, fmt.Sprintf("ada-%d@example.com", stamp)) {
		t.Fatalf("live login did not restore authenticated session: got %d: %s", response.StatusCode, body)
	}
	response, _ = requestJSON(t, plainClient, http.MethodGet, baseURL+"/health", "")
	if response.Header.Get("X-Request-ID") == "" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("production middleware headers are missing")
	}

	browser := clientWithCookiesNoRedirect(t)
	response, body = requestBrowser(t, browser, http.MethodGet, baseURL+"/register", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("browser registration form: expected 200, got %d: %s", response.StatusCode, body)
	}
	csrfToken := browserCSRF(t, body)
	response, body = requestBrowser(t, browser, http.MethodPost, baseURL+"/register", url.Values{
		"name": {"Browser User"}, "email": {fmt.Sprintf("browser-%d@example.com", stamp)}, "password": {"a browser passphrase"}, "password_confirmation": {"a browser passphrase"},
	})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("browser registration without CSRF: expected 403, got %d: %s", response.StatusCode, body)
	}
	const submittedPassword = "too-short"
	response, body = requestBrowser(t, browser, http.MethodPost, baseURL+"/register", url.Values{
		"_token": {csrfToken}, "name": {"B"}, "email": {fmt.Sprintf("browser-%d@example.com", stamp)}, "password": {submittedPassword}, "password_confirmation": {submittedPassword},
	})
	if response.StatusCode != http.StatusUnprocessableEntity || strings.Contains(body, submittedPassword) {
		t.Fatalf("browser validation did not safely re-render: %d: %s", response.StatusCode, body)
	}
	response, body = requestBrowser(t, browser, http.MethodPost, baseURL+"/register", url.Values{
		"_token": {csrfToken}, "name": {"Browser User"}, "email": {fmt.Sprintf("browser-%d@example.com", stamp)}, "password": {"a browser passphrase"}, "password_confirmation": {"a browser passphrase"},
	})
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/app" {
		t.Fatalf("browser registration: expected 303 /app, got %d %q: %s", response.StatusCode, response.Header.Get("Location"), body)
	}
	response, body = requestBrowser(t, browser, http.MethodGet, baseURL+"/app", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Welcome, Browser User") || !strings.Contains(body, "Welcome to GoForge.") {
		t.Fatalf("browser dashboard: %d: %s", response.StatusCode, body)
	}
	response, body = requestBrowser(t, browser, http.MethodGet, baseURL+"/app/issues/new", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("browser new issue: %d: %s", response.StatusCode, body)
	}
	csrfToken = browserCSRF(t, body)
	response, body = requestBrowser(t, browser, http.MethodPost, baseURL+"/app/issues", url.Values{
		"_token": {csrfToken}, "name": {"<strong>Browser issue</strong>"},
	})
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("browser create issue: %d: %s", response.StatusCode, body)
	}
	browserIssuePath := response.Header.Get("Location")
	if !strings.HasPrefix(browserIssuePath, "/app/issues/") {
		t.Fatalf("browser create location = %q", browserIssuePath)
	}
	response, body = requestBrowser(t, browser, http.MethodGet, baseURL+"/app/issues", nil)
	if response.StatusCode != http.StatusOK || strings.Contains(body, "<strong>Browser issue</strong>") || !strings.Contains(body, "&lt;strong&gt;Browser issue") {
		t.Fatalf("browser index did not list escaped issue: %d: %s", response.StatusCode, body)
	}
	response, body = requestBrowser(t, browser, http.MethodGet, baseURL+browserIssuePath, nil)
	if response.StatusCode != http.StatusOK || strings.Contains(body, "<strong>Browser issue</strong>") || !strings.Contains(body, "&lt;strong&gt;Browser issue") {
		t.Fatalf("browser show did not escape issue: %d: %s", response.StatusCode, body)
	}
	response, body = requestBrowser(t, second, http.MethodGet, baseURL+browserIssuePath, nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner browser read: expected 404, got %d: %s", response.StatusCode, body)
	}
	response, body = requestBrowser(t, browser, http.MethodGet, baseURL+browserIssuePath+"/edit", nil)
	if response.StatusCode != http.StatusOK || strings.Contains(body, "<strong>Browser issue</strong>") || !strings.Contains(body, "&lt;strong&gt;Browser issue") {
		t.Fatalf("browser edit did not render escaped issue: %d: %s", response.StatusCode, body)
	}
	csrfToken = browserCSRF(t, body)
	staleVersion := browserVersion(t, body)
	browserIssueAPIPath := strings.TrimPrefix(browserIssuePath, "/app")
	response, body = requestJSON(t, browser, http.MethodPut, baseURL+browserIssueAPIPath, `{"name":"Concurrent browser issue"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("concurrent browser issue update: expected 200, got %d: %s", response.StatusCode, body)
	}
	var browserWinner struct {
		Data struct {
			Version int64 `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &browserWinner); err != nil || browserWinner.Data.Version != staleVersion+1 {
		t.Fatalf("decode concurrent browser update: %v: %s", err, body)
	}
	response, body = requestBrowser(t, browser, http.MethodPost, baseURL+browserIssuePath, url.Values{
		"_token": {csrfToken}, "_method": {http.MethodPut}, "name": {"Browser issue updated"}, "version": {fmt.Sprint(staleVersion)},
	})
	if response.StatusCode != http.StatusConflict || !strings.Contains(body, "was changed by another request") || !strings.Contains(body, "Browser issue updated") {
		t.Fatalf("stale browser update did not safely re-render: %d: %s", response.StatusCode, body)
	}
	refreshedVersion := browserVersion(t, body)
	if refreshedVersion != browserWinner.Data.Version {
		t.Fatalf("stale browser form version = %d, want refreshed %d", refreshedVersion, browserWinner.Data.Version)
	}
	csrfToken = browserCSRF(t, body)
	response, body = requestBrowser(t, browser, http.MethodPost, baseURL+browserIssuePath, url.Values{
		"_token": {csrfToken}, "_method": {http.MethodPut}, "name": {"Browser issue updated"}, "version": {fmt.Sprint(refreshedVersion)},
	})
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("browser update retry: %d: %s", response.StatusCode, body)
	}
	response, body = requestBrowser(t, browser, http.MethodGet, baseURL+browserIssuePath, nil)
	csrfToken = browserCSRF(t, body)
	response, body = requestBrowser(t, browser, http.MethodPost, baseURL+browserIssuePath, url.Values{
		"_token": {csrfToken}, "_method": {http.MethodDelete},
	})
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/app/issues" {
		t.Fatalf("browser delete issue: %d %q: %s", response.StatusCode, response.Header.Get("Location"), body)
	}
	response, body = requestBrowser(t, browser, http.MethodGet, baseURL+"/app", nil)
	csrfToken = browserCSRF(t, body)
	response, body = requestBrowser(t, browser, http.MethodPost, baseURL+"/logout", url.Values{"_token": {csrfToken}})
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/login" {
		t.Fatalf("browser logout: %d %q: %s", response.StatusCode, response.Header.Get("Location"), body)
	}
	response, body = requestBrowser(t, browser, http.MethodGet, baseURL+"/login", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("browser login form: %d: %s", response.StatusCode, body)
	}
	csrfToken = browserCSRF(t, body)
	response, body = requestBrowser(t, browser, http.MethodPost, baseURL+"/login", url.Values{
		"_token": {csrfToken}, "email": {fmt.Sprintf("browser-%d@example.com", stamp)}, "password": {"a browser passphrase"},
	})
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/app" {
		t.Fatalf("browser login: %d %q: %s", response.StatusCode, response.Header.Get("Location"), body)
	}
	response, body = requestBrowser(t, browser, http.MethodGet, baseURL+"/app", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Welcome, Browser User") {
		t.Fatalf("browser login did not restore session: %d: %s", response.StatusCode, body)
	}
	t.Log("CSRF-protected HTML authentication and CRUD passed")
	t.Log("authenticated CRUD, ownership, validation, sessions, and middleware passed")
	stopCommandProcess(t, server, false)
	serverRunning = false
	t.Log("forge serve process stopped")

	// The production entrypoint itself must turn SIGTERM into graceful shutdown.
	binary := filepath.Join(directory, "app")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if output, err := generatedCommand(directory, environment, "go", "build", "-o", binary, "./cmd/server"); err != nil {
		t.Fatalf("build server: %v\n%s", err, output)
	}
	shutdownAddress := freeAddress(t)
	shutdownEnvironment := append(environment,
		"APP_ADDRESS="+shutdownAddress,
		"SESSION_SECRET="+strings.Repeat("s", 32),
	)
	shutdownServer := exec.Command(binary)
	shutdownServer.Dir = scratch
	shutdownServer.Env = shutdownEnvironment
	configureCommandProcess(shutdownServer)
	var shutdownOutput synchronizedBuffer
	shutdownServer.Stdout, shutdownServer.Stderr = &shutdownOutput, &shutdownOutput
	if err := shutdownServer.Start(); err != nil {
		t.Fatalf("start generated server for shutdown check: %v", err)
	}
	waitForHealth(t, "http://"+shutdownAddress, &shutdownOutput)
	standaloneClient := clientWithCookiesNoRedirect(t)
	response, body = requestBrowser(t, standaloneClient, http.MethodGet, "http://"+shutdownAddress+"/register", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Create account") {
		stopCommandProcess(t, shutdownServer, false)
		t.Fatalf("production binary did not render without source working directory: %d: %s", response.StatusCode, body)
	}
	stopCommandProcess(t, shutdownServer, true)

	if elapsed := time.Since(started); elapsed >= 10*time.Minute {
		t.Fatalf("four-command workflow took %s; milestone limit is under 10 minutes", elapsed)
	} else {
		t.Logf("four-command workflow completed in %s", elapsed.Round(time.Millisecond))
	}
}

func generatedCommand(directory string, environment []string, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = directory
	command.Env = environment
	output, err := command.CombinedOutput()
	return string(output), err
}

func postgresSchemaURL(databaseURL, schema string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("parse PostgreSQL URL: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("PostgreSQL URL must use postgres or postgresql scheme")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(contents []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(contents)
}

func (buffer *synchronizedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func waitForHealth(t *testing.T, baseURL string, serverOutput *synchronizedBuffer) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(baseURL + "/health")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server did not become healthy:\n%s", serverOutput.String())
}

func TestRelationshipAcceptanceFixtureEmitsInspectableGeneratedApp(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "relations")
	root := projectRoot(t)
	var output bytes.Buffer
	if err := Run([]string{"new", directory, "--module", "example.com/relations", "--replace", root}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if err := writeRelationshipAcceptanceFixture(directory, "example.com/relations"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	output.Reset()
	if err := Run([]string{"orm:generate"}, &output, &output); err != nil {
		t.Fatalf("generate relationship fixture: %v\n%s", err, output.String())
	}
	generated, err := os.ReadFile(filepath.Join("internal", "models", "zz_orm_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"func ProjectTenantRelation(",
		"func TenantProfileRelation(",
		"func TenantProjectsRelation(",
		"func ProjectTagsRelation(",
		"func (query ProjectQuery) AttachTags(",
		"func (query ProjectQuery) DetachTags(",
	} {
		if !strings.Contains(string(generated), expected) {
			t.Errorf("generated ORM is missing %q", expected)
		}
	}
	for _, path := range []string{
		filepath.Join("internal", "models", "tenant.go"),
		filepath.Join("internal", "models", "tenant_profile.go"),
		filepath.Join("internal", "models", "project.go"),
		filepath.Join("internal", "models", "tag.go"),
		filepath.Join("database", "migrations", "000003_relationship_acceptance.up.sql"),
		filepath.Join("database", "migrations", "000003_relationship_acceptance.down.sql"),
		filepath.Join(".forge", "relationship_acceptance.go"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("fixture did not emit %s: %v", path, err)
		}
	}
	baseEnvironment := append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if result, err := generatedCommand(directory, baseEnvironment, "go", "build", "./.forge/relationship_acceptance.go"); err != nil {
		t.Fatalf("relationship fixture does not compile: %v\n%s", err, result)
	}
}

func writeRelationshipAcceptanceFixture(directory, module string) error {
	files := map[string]string{
		filepath.Join("internal", "models", "tenant.go"):                                   relationshipTenantModel,
		filepath.Join("internal", "models", "tenant_profile.go"):                           relationshipTenantProfileModel,
		filepath.Join("internal", "models", "project.go"):                                  relationshipProjectModel,
		filepath.Join("internal", "models", "tag.go"):                                      relationshipTagModel,
		filepath.Join("database", "migrations", "000003_relationship_acceptance.up.sql"):   relationshipMigrationUp,
		filepath.Join("database", "migrations", "000003_relationship_acceptance.down.sql"): relationshipMigrationDown,
		filepath.Join(".forge", "relationship_acceptance.go"):                              strings.ReplaceAll(relationshipAcceptanceProgram, "example.com/issueboard", module),
	}
	for path, contents := range files {
		fullPath := filepath.Join(directory, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return fmt.Errorf("create relationship fixture directory: %w", err)
		}
		if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
			return fmt.Errorf("write relationship fixture %s: %w", path, err)
		}
	}
	return nil
}

const relationshipTenantModel = `package models

type Tenant struct {
	ID       int64          ` + "`forge:\"primary,generated,protected,required\"`" + `
	Name     string         ` + "`forge:\"required\"`" + `
	Profile  *TenantProfile ` + "`forge:\"has_one,target=TenantProfile,foreign_key=TenantID,references=ID\"`" + `
	Projects []*Project     ` + "`forge:\"has_many,target=Project,foreign_key=TenantID,references=ID\"`" + `
}
`

const relationshipTenantProfileModel = `package models

type TenantProfile struct {
	ID       int64   ` + "`forge:\"primary,generated,protected,required\"`" + `
	TenantID *int64  ` + "`forge:\"nullable,unique,references=Tenant.ID,on_delete=set_null\"`" + `
	Bio      string  ` + "`forge:\"required\"`" + `
	Tenant   *Tenant ` + "`forge:\"belongs_to,target=Tenant,foreign_key=TenantID,references=ID\"`" + `
}
`

const relationshipProjectModel = `package models

type Project struct {
	ID       int64   ` + "`forge:\"primary,generated,protected,required\"`" + `
	TenantID int64   ` + "`forge:\"protected,required,index,references=Tenant.ID,on_delete=cascade\"`" + `
	Name     string  ` + "`forge:\"required\"`" + `
	Tenant   *Tenant ` + "`forge:\"belongs_to,target=Tenant,foreign_key=TenantID,references=ID\"`" + `
	Tags     []*Tag  ` + "`forge:\"many_to_many,target=Tag,references=ID,target_key=ID,join_table=project_tags,join_foreign_key=project_id,join_reference_key=tag_id\"`" + `
}
`

const relationshipTagModel = `package models

type Tag struct {
	ID       int64  ` + "`forge:\"primary,generated,protected,required\"`" + `
	TenantID int64  ` + "`forge:\"protected,required,index,references=Tenant.ID,on_delete=cascade\"`" + `
	Name     string ` + "`forge:\"required\"`" + `
}
`

const relationshipMigrationUp = `CREATE TABLE tenants (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE tenant_profiles (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT UNIQUE REFERENCES tenants(id) ON DELETE SET NULL,
    bio TEXT NOT NULL
);

CREATE TABLE projects (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name TEXT NOT NULL
);

CREATE INDEX projects_tenant_id_idx ON projects (tenant_id);

CREATE TABLE tags (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    UNIQUE (tenant_id, name)
);

CREATE INDEX tags_tenant_id_idx ON tags (tenant_id);

CREATE TABLE project_tags (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    tag_id BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, tag_id)
);
`

const relationshipMigrationDown = `DROP TABLE IF EXISTS project_tags;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS tenant_profiles;
DROP TABLE IF EXISTS tenants;
`

const relationshipAcceptanceProgram = `package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"example.com/issueboard/internal/models"
	"github.com/ShanilKoshitha/goforge/orm"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type observation struct {
	event orm.StatementEvent
	sql   string
}

func main() {
	ctx := context.Background()
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		panic(err)
	}

	tenantA := insertID(ctx, db, "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "tenant-a")
	tenantB := insertID(ctx, db, "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "tenant-b")
	profileA := insertID(ctx, db, "INSERT INTO tenant_profiles (tenant_id, bio) VALUES ($1, $2) RETURNING id", tenantA, "visible")
	orphanProfile := insertID(ctx, db, "INSERT INTO tenant_profiles (tenant_id, bio) VALUES (NULL, $1) RETURNING id", "orphan")
	projectA := insertID(ctx, db, "INSERT INTO projects (tenant_id, name) VALUES ($1, $2) RETURNING id", tenantA, "project-a")
	projectB := insertID(ctx, db, "INSERT INTO projects (tenant_id, name) VALUES ($1, $2) RETURNING id", tenantB, "project-b")
	tagA := insertID(ctx, db, "INSERT INTO tags (tenant_id, name) VALUES ($1, $2) RETURNING id", tenantA, "tag-a")
	tagB := insertID(ctx, db, "INSERT INTO tags (tenant_id, name) VALUES ($1, $2) RETURNING id", tenantB, "tag-b")

	var observed []observation
	executor := orm.ObserveExecutor(db, func(_ context.Context, event orm.StatementEvent, statement orm.Statement) {
		observed = append(observed, observation{event: event, sql: statement.SQL()})
	})
	store := models.NewStore(executor)

	attached, err := store.Projects().AttachTags(ctx, projectA, tagA)
	if err != nil || !attached {
		panic(fmt.Sprintf("attach local tag: attached=%v err=%v", attached, err))
	}
	attached, err = store.Projects().AttachTags(ctx, projectA, tagB)
	if err != nil || !attached {
		panic(fmt.Sprintf("attach foreign tag fixture: attached=%v err=%v", attached, err))
	}

	before := len(observed)
	profiles, err := store.TenantProfiles().LoadTenant(ctx,
		[]models.TenantProfile{{ID: profileA, TenantID: &tenantA}, {ID: orphanProfile}},
		orm.Select(models.TenantTable).Where(models.TenantColumns.ID.Eq(tenantA)),
	)
	if err != nil {
		panic(err)
	}
	expectOneScopedQuery(observed, before, ` + "`\"tenants\".\"id\" = $1`" + `, "nullable belongs-to")
	if profiles[0].Tenant == nil || profiles[0].Tenant.ID != tenantA || profiles[1].Tenant != nil {
		panic(fmt.Sprintf("nullable belongs-to assignment leaked or mismatched: %#v", profiles))
	}

	before = len(observed)
	tenants, err := store.Tenants().LoadProfile(ctx,
		[]models.Tenant{{ID: tenantA}, {ID: tenantB}},
		orm.Select(models.TenantProfileTable).Where(models.TenantProfileColumns.TenantID.Eq(&tenantA)),
	)
	if err != nil {
		panic(err)
	}
	expectOneScopedQuery(observed, before, ` + "`\"tenant_profiles\".\"tenant_id\" = $1`" + `, "has-one")
	if tenants[0].Profile == nil || tenants[0].Profile.ID != profileA || tenants[1].Profile != nil {
		panic(fmt.Sprintf("tenant-scoped has-one leaked or mismatched: %#v", tenants))
	}

	before = len(observed)
	tenants, err = store.Tenants().LoadProjects(ctx,
		[]models.Tenant{{ID: tenantA}, {ID: tenantB}},
		orm.Select(models.ProjectTable).Where(models.ProjectColumns.TenantID.Eq(tenantA)),
	)
	if err != nil {
		panic(err)
	}
	expectOneScopedQuery(observed, before, ` + "`\"projects\".\"tenant_id\" = $1`" + `, "has-many")
	if len(tenants[0].Projects) != 1 || tenants[0].Projects[0].ID != projectA || len(tenants[1].Projects) != 0 {
		panic(fmt.Sprintf("tenant-scoped has-many leaked or mismatched: %#v", tenants))
	}

	before = len(observed)
	projects, err := store.Projects().LoadTags(ctx,
		[]models.Project{{ID: projectA, TenantID: tenantA}, {ID: projectB, TenantID: tenantB}},
		orm.Select(models.TagTable).Where(models.TagColumns.TenantID.Eq(tenantA)),
	)
	if err != nil {
		panic(err)
	}
	expectOneScopedQuery(observed, before, ` + "`\"tags\".\"tenant_id\" = $1`" + `, "many-to-many")
	if len(projects[0].Tags) != 1 || projects[0].Tags[0].ID != tagA || len(projects[1].Tags) != 0 {
		panic(fmt.Sprintf("tenant-scoped many-to-many leaked or mismatched: %#v", projects))
	}

	detached, err := store.Projects().DetachTags(ctx, projectA, tagB)
	if err != nil || !detached {
		panic(fmt.Sprintf("detach foreign tag fixture: detached=%v err=%v", detached, err))
	}
	queries, executions := 0, 0
	for _, item := range observed {
		switch item.event {
		case orm.StatementQuery:
			queries++
		case orm.StatementExec:
			executions++
		}
	}
	if queries != 4 || executions != 3 || len(observed) != 7 {
		panic(fmt.Sprintf("relationship query budget: queries=%d executions=%d total=%d", queries, executions, len(observed)))
	}
	fmt.Println("relationship acceptance passed")
}

func insertID(ctx context.Context, db *sql.DB, query string, args ...any) int64 {
	var id int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
		panic(err)
	}
	return id
}

func expectOneScopedQuery(observed []observation, before int, predicate, label string) {
	if len(observed) != before+1 || observed[before].event != orm.StatementQuery {
		panic(fmt.Sprintf("%s budget: before=%d after=%d", label, before, len(observed)))
	}
	if !strings.Contains(observed[before].sql, predicate) {
		panic(fmt.Sprintf("%s lost target scope: %s", label, observed[before].sql))
	}
}
`

const postgresAdminProgram = `package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	defer db.Close()
	ctx := context.Background()
	if len(os.Args) == 3 && (os.Args[1] == "create" || os.Args[1] == "drop") {
		schema := os.Args[2]
		if !regexp.MustCompile("^[a-z][a-z0-9_]+$").MatchString(schema) {
			panic("unsafe schema name")
		}
		statement := fmt.Sprintf("CREATE SCHEMA %s", schema)
		if os.Args[1] == "drop" {
			statement = fmt.Sprintf("DROP SCHEMA %s CASCADE", schema)
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			panic(err)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "seed-expired-session" {
		if _, err := db.ExecContext(ctx, "INSERT INTO sessions (id, payload, expires_at) VALUES ($1, $2, NOW() - INTERVAL '1 hour')", "abandoned-expired", []byte("{}")); err != nil {
			panic(err)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "count-expired-sessions" {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE expires_at <= NOW()").Scan(&count); err != nil {
			panic(err)
		}
		fmt.Println(count)
		return
	}
	panic("usage: acceptance_db <create|drop> <schema> | <seed-expired-session|count-expired-sessions>")
}
`

func clientWithCookies(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 3 * time.Second}
}

func requestJSON(t *testing.T, client *http.Client, method, url, body string) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(contents)
}

var (
	browserCSRFPattern    = regexp.MustCompile(`name="_token" value="([^"]+)"`)
	browserVersionPattern = regexp.MustCompile(`name="version" value="([0-9]+)"`)
)

func clientWithCookiesNoRedirect(t *testing.T) *http.Client {
	t.Helper()
	client := clientWithCookies(t)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

func browserCSRF(t *testing.T, body string) string {
	t.Helper()
	match := browserCSRFPattern.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("browser response has no CSRF token: %s", body)
	}
	return match[1]
}

func browserVersion(t *testing.T, body string) int64 {
	t.Helper()
	match := browserVersionPattern.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("browser response has no version field: %s", body)
	}
	version, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || version < 1 {
		t.Fatalf("browser response has invalid version %q: %v", match[1], err)
	}
	return version
}

func requestBrowser(t *testing.T, client *http.Client, method, target string, values url.Values) (*http.Response, string) {
	t.Helper()
	body := ""
	if values != nil {
		body = values.Encode()
	}
	request, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if values != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(contents)
}
