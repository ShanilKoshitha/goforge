package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/mod/modfile"
)

func TestGeneratedScalableResourcesPostgresWorkflow(t *testing.T) {
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

	directory := filepath.Join(scratch, "scalable-resources")
	if output, err := generatedCommand(scratch, baseEnvironment, forgeBinary, "new", "scalable-resources", "--module", "example.com/scalable", "--replace", root); err != nil {
		t.Fatalf("forge new: %v\n%s", err, output)
	}
	assertScalableFreshScaffoldContract(t, directory, root)
	commands := [][]string{
		{"make:resource", "Category", "--field", "title:string"},
		{"make:resource", "Project", "--field", "private_label:string"},
		{
			"make:resource", "Ticket",
			"--field", "title:string",
			"--field", "notes:text:nullable",
			"--field", "priority:integer",
			"--field", "estimate:integer:nullable",
			"--field", "active:boolean",
			"--field", "featured:boolean:nullable",
			"--belongs-to", "category:Category",
			"--belongs-to", "project:Project",
			"--choice-label", "category:title",
		},
	}
	for _, arguments := range commands {
		if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, arguments...); err != nil {
			t.Fatalf("forge %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
	}
	customizeScalableTicketAuthorization(t, directory)
	customizeScalableTargetListAuthorization(t, directory, "category")
	customizeScalableTargetListAuthorization(t, directory, "project")
	proofPath := filepath.Join(directory, ".forge", "scalable_resources_acceptance.go")
	if err := os.WriteFile(proofPath, []byte(scalableResourcesAcceptanceProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := generatedCommand(directory, baseEnvironment, "go", "test", "./..."); err != nil {
		t.Fatalf("generated scalable application tests: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, "go", "build", "./.forge/scalable_resources_acceptance.go"); err != nil {
		t.Fatalf("generated scalable repository proof does not compile: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "build"); err != nil {
		t.Fatalf("build generated scalable application: %v\n%s", err, output)
	}

	schema := fmt.Sprintf("goforge_scalable_resources_%d", time.Now().UnixNano())
	adminPath := filepath.Join(directory, ".forge", "acceptance_db.go")
	adminSource := strings.ReplaceAll(postgresAdminProgram, "example.com/issueboard", "example.com/scalable")
	if err := os.WriteFile(adminPath, []byte(adminSource), 0o644); err != nil {
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
	environment := append(baseEnvironment, "DATABASE_URL="+isolatedURL, "APP_ENV=local")
	if output, err := generatedCommand(directory, environment, forgeBinary, "migrate"); err != nil {
		t.Fatalf("migrate generated scalable application: %v\n%s", err, output)
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

	owner := clientWithCookiesNoRedirect(t)
	foreign := clientWithCookiesNoRedirect(t)
	admin := clientWithCookiesNoRedirect(t)
	registerScalableUser(t, owner, baseURL, "Scale Owner", "scale-owner@example.com")
	registerScalableUser(t, foreign, baseURL, "Foreign Owner", "foreign-owner@example.com")
	registerScalableUser(t, admin, baseURL, "Administrator", "admin@example.com")

	database, err := sql.Open("pgx", isolatedURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	seed := seedScalableResourceRows(t, ctx, database)

	beforeRefreshes := scalableTicketCount(t, ctx, database)
	testScalableChoiceReachability(t, owner, baseURL, seed)
	if after := scalableTicketCount(t, ctx, database); after != beforeRefreshes {
		t.Fatalf("choice refreshes wrote tickets: before=%d after=%d", beforeRefreshes, after)
	}
	testScalableChoiceSelectionPersistence(t, owner, baseURL, seed)

	created := testScalableCorrectableBrowserCreate(t, owner, baseURL, seed, database)
	testScalableExplicitIntentAndCSRF(t, owner, baseURL, seed, database)
	testScalableStaleBrowserUpdate(t, owner, baseURL, seed, created)
	testScalableOwnerAndPrivilegedChoices(t, owner, foreign, admin, baseURL, seed, created)
	testScalableListParity(t, owner, foreign, baseURL, seed, database)
	testScalableInputRejection(t, owner, baseURL, seed, database)
	if output, err := generatedCommand(directory, environment, "go", "run", "./.forge/scalable_resources_acceptance.go",
		strconv.FormatInt(seed.OwnerID, 10), strconv.FormatInt(seed.FirstCategory, 10), strconv.FormatInt(seed.LastCategory, 10),
		strconv.FormatInt(seed.OwnerProject, 10), strconv.FormatInt(seed.OwnerProjectLast, 10)); err != nil {
		t.Fatalf("real PostgreSQL scalable repository proof: %v\n%s", err, output)
	} else if strings.TrimSpace(output) != "scalable resources acceptance passed" {
		t.Fatalf("unexpected scalable repository proof output: %s", output)
	}
}

func assertScalableFreshScaffoldContract(t *testing.T, directory, root string) {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(directory, "forge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(manifest), "version: 17\n") {
		t.Fatalf("fresh scalable application is not format 17:\n%s", manifest)
	}

	moduleSource, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	module, err := modfile.Parse("go.mod", moduleSource, nil)
	if err != nil {
		t.Fatal(err)
	}
	const runtimeModule = "github.com/ShanilKoshitha/goforge"
	var runtimeVersion string
	for _, requirement := range module.Require {
		if requirement.Mod.Path == runtimeModule {
			runtimeVersion = requirement.Mod.Version
			break
		}
	}
	if runtimeVersion != "v0.23.0" {
		t.Fatalf("fresh scalable application runtime = %q, want public v0.23.0:\n%s", runtimeVersion, moduleSource)
	}
	var replacement string
	for _, directive := range module.Replace {
		if directive.Old.Path == runtimeModule && directive.Old.Version == "" {
			replacement = directive.New.Path
			break
		}
	}
	if replacement == "" || filepath.Clean(replacement) != filepath.Clean(root) {
		t.Fatalf("fresh scalable harness replacement = %q, want local source %q:\n%s", replacement, root, moduleSource)
	}

	sums, err := os.ReadFile(filepath.Join(directory, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	wantSums := map[string]bool{
		"github.com/ShanilKoshitha/goforge v0.23.0 h1:bjSSvFM3rBH/aJ2TesGxO7n8vcG0cPJUQVPgAT9SotY=":        false,
		"github.com/ShanilKoshitha/goforge v0.23.0/go.mod h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=": false,
	}
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		if !strings.HasPrefix(line, runtimeModule+" ") {
			continue
		}
		if _, expected := wantSums[line]; !expected {
			t.Fatalf("fresh scalable application contains an unexpected GoForge checksum %q", line)
		}
		wantSums[line] = true
	}
	for line, found := range wantSums {
		if !found {
			t.Errorf("fresh scalable application omits public runtime checksum %q", line)
		}
	}
}

type scalableSeed struct {
	OwnerID, ForeignID, AdminID                                     int64
	FirstCategory, MiddleCategory, LastCategory                     int64
	EqualCategoryOne, EqualCategoryTwo                              int64
	ForeignCategory, AdminCategory, UnsafeCategory, DeletedCategory int64
	OwnerProject, OwnerProjectLast, ForeignProject                  int64
}

type scalableTicket struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Title      string  `json:"title"`
	Notes      *string `json:"notes"`
	Priority   int64   `json:"priority"`
	Estimate   *int64  `json:"estimate"`
	Active     bool    `json:"active"`
	Featured   *bool   `json:"featured"`
	CategoryID int64   `json:"category_id"`
	ProjectID  int64   `json:"project_id"`
	Version    int64   `json:"version"`
}

type scalableTicketPage struct {
	Data       []scalableTicket `json:"data"`
	Pagination struct {
		Page        int  `json:"page"`
		PerPage     int  `json:"per_page"`
		HasPrevious bool `json:"has_previous"`
		HasNext     bool `json:"has_next"`
	} `json:"pagination"`
}

func registerScalableUser(t *testing.T, client *http.Client, baseURL, name, email string) {
	t.Helper()
	response, body := requestJSON(t, client, http.MethodPost, baseURL+"/auth/register",
		fmt.Sprintf(`{"name":%q,"email":%q,"password":"a secure passphrase","password_confirmation":"a secure passphrase"}`, name, email))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register scalable user %s: got %d: %s", email, response.StatusCode, body)
	}
}

func seedScalableResourceRows(t *testing.T, ctx context.Context, database *sql.DB) scalableSeed {
	t.Helper()
	seed := scalableSeed{
		OwnerID:   scalableUserID(t, ctx, database, "scale-owner@example.com"),
		ForeignID: scalableUserID(t, ctx, database, "foreign-owner@example.com"),
		AdminID:   scalableUserID(t, ctx, database, "admin@example.com"),
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO categories (user_id, title)
		SELECT $1, 'Category ' || lpad(value::text, 5, '0')
		FROM generate_series(1, 10000) AS value`, seed.OwnerID); err != nil {
		t.Fatalf("seed 10,000 owner categories: %v", err)
	}
	updates := map[string]string{
		"Category 00001": "First searchable category",
		"Category 05000": "Middle searchable category",
		"Category 10000": "Last searchable category",
		"Category 00400": "Equal searchable category",
		"Category 00401": "Equal searchable category",
		"Category 00402": "<b>" + strings.Repeat("界", 130),
	}
	for oldTitle, newTitle := range updates {
		if _, err := database.ExecContext(ctx, "UPDATE categories SET title = $1 WHERE user_id = $2 AND title = $3", newTitle, seed.OwnerID, oldTitle); err != nil {
			t.Fatalf("update category fixture %q: %v", oldTitle, err)
		}
	}
	seed.FirstCategory = scalableCategoryID(t, ctx, database, seed.OwnerID, "First searchable category", 0)
	seed.MiddleCategory = scalableCategoryID(t, ctx, database, seed.OwnerID, "Middle searchable category", 0)
	seed.LastCategory = scalableCategoryID(t, ctx, database, seed.OwnerID, "Last searchable category", 0)
	seed.EqualCategoryOne = scalableCategoryID(t, ctx, database, seed.OwnerID, "Equal searchable category", 0)
	seed.EqualCategoryTwo = scalableCategoryID(t, ctx, database, seed.OwnerID, "Equal searchable category", seed.EqualCategoryOne)
	seed.UnsafeCategory = scalableCategoryID(t, ctx, database, seed.OwnerID, "<b>"+strings.Repeat("界", 130), 0)
	if err := database.QueryRowContext(ctx, "INSERT INTO categories (user_id, title) VALUES ($1, $2) RETURNING id", seed.ForeignID, "Foreign private category").Scan(&seed.ForeignCategory); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, "INSERT INTO categories (user_id, title) VALUES ($1, $2) RETURNING id", seed.AdminID, "Admin private category").Scan(&seed.AdminCategory); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, "INSERT INTO categories (user_id, title) VALUES ($1, $2) RETURNING id", seed.OwnerID, "Deleted private category").Scan(&seed.DeletedCategory); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM categories WHERE id = $1", seed.DeletedCategory); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 30; index++ {
		var id int64
		label := fmt.Sprintf("PROJECT_PRIVATE_%02d", index)
		if err := database.QueryRowContext(ctx, "INSERT INTO projects (user_id, private_label) VALUES ($1, $2) RETURNING id", seed.OwnerID, label).Scan(&id); err != nil {
			t.Fatalf("seed owner project %d: %v", index, err)
		}
		if index == 1 {
			seed.OwnerProject = id
		}
		if index == 30 {
			seed.OwnerProjectLast = id
		}
	}
	if err := database.QueryRowContext(ctx, "INSERT INTO projects (user_id, private_label) VALUES ($1, $2) RETURNING id", seed.ForeignID, "FOREIGN_PROJECT_PRIVATE").Scan(&seed.ForeignProject); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO projects (user_id, private_label) VALUES ($1, $2)", seed.AdminID, "ADMIN_PROJECT_PRIVATE"); err != nil {
		t.Fatal(err)
	}
	return seed
}

func scalableUserID(t *testing.T, ctx context.Context, database *sql.DB, email string) int64 {
	t.Helper()
	var id int64
	if err := database.QueryRowContext(ctx, "SELECT id FROM users WHERE email = $1", email).Scan(&id); err != nil || id < 1 {
		t.Fatalf("lookup scalable user %q: id=%d err=%v", email, id, err)
	}
	return id
}

func scalableCategoryID(t *testing.T, ctx context.Context, database *sql.DB, userID int64, title string, after int64) int64 {
	t.Helper()
	var id int64
	if err := database.QueryRowContext(ctx, "SELECT id FROM categories WHERE user_id = $1 AND title = $2 AND id > $3 ORDER BY id LIMIT 1", userID, title, after).Scan(&id); err != nil {
		t.Fatalf("lookup scalable category %q after %d: %v", title, after, err)
	}
	return id
}

func testScalableChoiceReachability(t *testing.T, client *http.Client, baseURL string, seed scalableSeed) {
	t.Helper()
	response, body := requestBrowser(t, client, http.MethodGet, baseURL+"/app/tickets/new", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("new scalable ticket form: got %d: %s", response.StatusCode, body)
	}
	for _, marker := range []string{
		`<form action="/app/tickets" method="post">`,
		`name="intent" value="save"`,
		`name="intent" value="choice:category:search"`,
		`name="intent" value="choice:category:lookup"`,
		`name="intent" value="choice:category:next"`,
		`name="choice[project][id]"`,
		"formnovalidate",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("new form omits scalable control %q: %s", marker, body)
		}
	}
	if strings.Contains(body, "PROJECT_PRIVATE_") || strings.Contains(body, "Foreign private category") {
		t.Fatalf("new form disclosed an ID-only label or foreign choice: %s", body)
	}

	values := scalableTicketForm(browserCSRF(t, body), seed)
	values.Set("title", "<dirty & unfinished>")
	values.Set("estimate", "not-an-integer")
	values.Set("category_id", strconv.FormatInt(seed.FirstCategory, 10))
	values.Set("intent", "choice:project:next")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusOK || scalableInputValue(t, body, "choice[project][page]") != "2" ||
		!strings.Contains(body, `name="intent" value="choice:project:previous"`) {
		t.Fatalf("project next-page refresh did not render page 2 and Previous: got %d: %s", response.StatusCode, body)
	}
	assertScalableChoice(t, body, "project_id", seed.OwnerProject, "", true)
	assertScalableChoice(t, body, "project_id", seed.OwnerProjectLast, "", false)
	if strings.Contains(body, "PROJECT_PRIVATE_") {
		t.Fatalf("ID-only project page disclosed private text: %s", body)
	}
	values.Set("_token", browserCSRF(t, body))
	values.Set("choice[project][page]", "2")
	values.Set("intent", "choice:project:previous")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusOK || scalableInputValue(t, body, "choice[project][page]") != "1" ||
		!strings.Contains(body, `value="&lt;dirty &amp; unfinished&gt;"`) {
		t.Fatalf("project Previous did not preserve dirty form state and return to page 1: got %d: %s", response.StatusCode, body)
	}
	values.Set("_token", browserCSRF(t, body))
	values.Set("choice[project][page]", "1")
	values.Set("intent", "choice:project:next")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusOK || scalableInputValue(t, body, "choice[project][page]") != "2" {
		t.Fatalf("project Next did not restore page 2: got %d: %s", response.StatusCode, body)
	}

	values.Set("_token", browserCSRF(t, body))
	values.Set("choice[category][q]", "Last searchable")
	values.Set("choice[category][id]", strconv.FormatInt(seed.MiddleCategory, 10))
	values.Set("choice[project][page]", "2")
	values.Set("intent", "choice:category:search")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `value="&lt;dirty &amp; unfinished&gt;"`) || !strings.Contains(body, `value="not-an-integer"`) {
		t.Fatalf("choice search did not preserve recoverable dirty form values: got %d: %s", response.StatusCode, body)
	}
	if scalableInputValue(t, body, "priority") != "0" || scalableTextareaValue(t, body, "notes") != "" ||
		scalableSelectedOption(t, body, "active") != "false" || scalableSelectedOption(t, body, "featured") != "" {
		t.Fatalf("choice search did not preserve zero/false/empty/null scalar state: %s", body)
	}
	assertScalableChoice(t, body, "category_id", seed.LastCategory, "Last searchable category", false)
	assertScalableChoice(t, body, "category_id", seed.FirstCategory, "First searchable category", true)
	if got := scalableInputValue(t, body, "choice[category][id]"); got != "" {
		t.Fatalf("text search retained stale category ID filter %q", got)
	}
	if got := scalableInputValue(t, body, "choice[project][page]"); got != "2" {
		t.Fatalf("category search changed independent project page to %q", got)
	}

	for _, target := range []struct {
		id    int64
		query string
	}{
		{seed.FirstCategory, "First searchable"},
		{seed.MiddleCategory, "Middle searchable"},
		{seed.LastCategory, "Last searchable"},
	} {
		values = scalableTicketForm(browserCSRF(t, body), seed)
		values.Set("choice[category][q]", target.query)
		values.Set("intent", "choice:category:search")
		response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("text choice lookup %q: got %d: %s", target.query, response.StatusCode, body)
		}
		assertScalableChoice(t, body, "category_id", target.id, target.query, target.id == seed.FirstCategory)

		values.Set("_token", browserCSRF(t, body))
		values.Set("choice[category][q]", "stale text must clear")
		values.Set("choice[category][id]", strconv.FormatInt(target.id, 10))
		values.Set("intent", "choice:category:lookup")
		response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("ID choice lookup %d: got %d: %s", target.id, response.StatusCode, body)
		}
		assertScalableChoice(t, body, "category_id", target.id, target.query, target.id == seed.FirstCategory)
		if got := scalableInputValue(t, body, "choice[category][q]"); got != "" {
			t.Fatalf("ID lookup retained stale category text filter %q", got)
		}
	}

	values = scalableTicketForm(browserCSRF(t, body), seed)
	values.Set("choice[category][q]", "Equal searchable category")
	values.Set("intent", "choice:category:search")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("equal-label search: got %d: %s", response.StatusCode, body)
	}
	assertScalableChoice(t, body, "category_id", seed.EqualCategoryOne, "Equal searchable category", false)
	assertScalableChoice(t, body, "category_id", seed.EqualCategoryTwo, "Equal searchable category", false)

	values.Set("_token", browserCSRF(t, body))
	values.Set("choice[category][q]", "<b>")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unsafe long-label search: got %d: %s", response.StatusCode, body)
	}
	label := assertScalableChoice(t, body, "category_id", seed.UnsafeCategory, "<b>", false)
	if strings.Contains(body, "<b>") || !strings.Contains(body, "&lt;b&gt;") || utf8.RuneCountInString(label) > 160 {
		t.Fatalf("choice label was not escaped and bounded: runes=%d label=%q body=%s", utf8.RuneCountInString(label), label, body)
	}
}

func testScalableChoiceSelectionPersistence(t *testing.T, client *http.Client, baseURL string, seed scalableSeed) {
	t.Helper()
	targets := []struct {
		name  string
		id    int64
		query string
	}{
		{name: "first", id: seed.FirstCategory, query: "First searchable"},
		{name: "middle", id: seed.MiddleCategory, query: "Middle searchable"},
		{name: "last", id: seed.LastCategory, query: "Last searchable"},
	}
	for _, method := range []string{"text", "id"} {
		for _, target := range targets {
			t.Run(method+"_"+target.name, func(t *testing.T) {
				response, body := requestBrowser(t, client, http.MethodGet, baseURL+"/app/tickets/new", nil)
				if response.StatusCode != http.StatusOK {
					t.Fatalf("open selection form: got %d: %s", response.StatusCode, body)
				}
				values := scalableTicketForm(browserCSRF(t, body), seed)
				values.Set("title", fmt.Sprintf("Persisted %s choice via %s", target.name, method))
				switch method {
				case "text":
					values.Set("choice[category][q]", target.query)
					values.Set("intent", "choice:category:search")
				case "id":
					values.Set("choice[category][id]", strconv.FormatInt(target.id, 10))
					values.Set("intent", "choice:category:lookup")
				default:
					t.Fatalf("unsupported choice method %q", method)
				}
				response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
				if response.StatusCode != http.StatusOK {
					t.Fatalf("%s choice refresh for %s: got %d: %s", method, target.name, response.StatusCode, body)
				}
				assertScalableChoice(t, body, "category_id", target.id, target.query, target.id == seed.FirstCategory)

				values.Set("_token", browserCSRF(t, body))
				values.Set("category_id", strconv.FormatInt(target.id, 10))
				values.Set("intent", "save")
				response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
				if response.StatusCode != http.StatusSeeOther {
					t.Fatalf("persist %s choice for %s: got %d location=%q: %s", method, target.name, response.StatusCode, response.Header.Get("Location"), body)
				}
				id := scalableLocationID(t, response.Header.Get("Location"), "/app/tickets/")
				response, body = requestJSON(t, client, http.MethodGet, fmt.Sprintf("%s/tickets/%d", baseURL, id), "")
				if response.StatusCode != http.StatusOK {
					t.Fatalf("read persisted %s choice for %s: got %d: %s", method, target.name, response.StatusCode, body)
				}
				item := decodeScalableTicket(t, body)
				if item.CategoryID != target.id {
					t.Fatalf("persisted %s choice for %s = category %d, want %d", method, target.name, item.CategoryID, target.id)
				}
			})
		}
	}
}

func testScalableCorrectableBrowserCreate(t *testing.T, client *http.Client, baseURL string, seed scalableSeed, database *sql.DB) scalableTicket {
	t.Helper()
	response, body := requestBrowser(t, client, http.MethodGet, baseURL+"/app/tickets/new", nil)
	values := scalableTicketForm(browserCSRF(t, body), seed)
	values.Set("title", "")
	values.Set("estimate", "broken-estimate")
	values.Set("validation_attempted", "0")
	values.Set("intent", "save")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusUnprocessableEntity || scalableInputValue(t, body, "validation_attempted") != "1" {
		t.Fatalf("invalid save did not enter correctable 422 state: got %d: %s", response.StatusCode, body)
	}
	before := scalableTicketCount(t, context.Background(), database)
	values.Set("_token", browserCSRF(t, body))
	values.Set("choice[category][id]", strconv.FormatInt(seed.MiddleCategory, 10))
	values.Set("intent", "choice:category:lookup")
	values.Set("validation_attempted", "1")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "broken-estimate") {
		t.Fatalf("422 choice refresh lost recoverable errors/values: got %d: %s", response.StatusCode, body)
	}
	if after := scalableTicketCount(t, context.Background(), database); after != before {
		t.Fatalf("422 choice refresh wrote a ticket: before=%d after=%d", before, after)
	}
	values.Set("_token", browserCSRF(t, body))
	values.Set("title", "Corrected browser ticket")
	values.Set("estimate", "")
	values.Set("category_id", strconv.FormatInt(seed.MiddleCategory, 10))
	values.Set("intent", "save")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("corrected browser ticket save: got %d location=%q: %s", response.StatusCode, response.Header.Get("Location"), body)
	}
	id := scalableLocationID(t, response.Header.Get("Location"), "/app/tickets/")
	response, body = requestJSON(t, client, http.MethodGet, fmt.Sprintf("%s/tickets/%d", baseURL, id), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("read corrected browser ticket: got %d: %s", response.StatusCode, body)
	}
	return decodeScalableTicket(t, body)
}

func testScalableExplicitIntentAndCSRF(t *testing.T, client *http.Client, baseURL string, seed scalableSeed, database *sql.DB) {
	t.Helper()
	response, body := requestBrowser(t, client, http.MethodGet, baseURL+"/app/tickets/new", nil)
	values := scalableTicketForm(browserCSRF(t, body), seed)
	before := scalableTicketCount(t, context.Background(), database)

	values.Del("intent")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing intent: got %d: %s", response.StatusCode, body)
	}
	values["intent"] = []string{"save", "choice:category:search"}
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate intent: got %d: %s", response.StatusCode, body)
	}
	values.Set("intent", "unrecognized")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown intent: got %d: %s", response.StatusCode, body)
	}
	values.Set("intent", "save")
	values.Del("_token")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF: got %d: %s", response.StatusCode, body)
	}
	values.Set("intent", "choice:category:search")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("choice refresh without CSRF: got %d: %s", response.StatusCode, body)
	}
	if after := scalableTicketCount(t, context.Background(), database); after != before {
		t.Fatalf("rejected intent/CSRF requests wrote tickets: before=%d after=%d", before, after)
	}
}

func testScalableStaleBrowserUpdate(t *testing.T, client *http.Client, baseURL string, seed scalableSeed, ticket scalableTicket) {
	t.Helper()
	path := fmt.Sprintf("/app/tickets/%d", ticket.ID)
	response, body := requestBrowser(t, client, http.MethodGet, baseURL+path+"/edit", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("open scalable ticket edit: got %d: %s", response.StatusCode, body)
	}
	csrf := browserCSRF(t, body)
	staleVersion := browserVersion(t, body)
	payload := scalableTicketJSON("Concurrent update", nil, 3, nil, true, nil, seed.MiddleCategory, seed.OwnerProject, &staleVersion)
	response, body = requestJSON(t, client, http.MethodPut, baseURL+fmt.Sprintf("/tickets/%d", ticket.ID), payload)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("concurrent scalable ticket update: got %d: %s", response.StatusCode, body)
	}
	current := decodeScalableTicket(t, body)

	values := scalableTicketForm(csrf, seed)
	values.Set("_method", http.MethodPut)
	values.Set("version", strconv.FormatInt(staleVersion, 10))
	values.Set("title", "Dirty stale correction")
	values.Set("category_id", strconv.FormatInt(seed.MiddleCategory, 10))
	values.Set("choice[category][q]", "Last searchable")
	values.Set("intent", "choice:category:search")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+path, values)
	if response.StatusCode != http.StatusOK || browserVersion(t, body) != staleVersion {
		t.Fatalf("choice refresh did not retain valid stale version %d: got %d: %s", staleVersion, response.StatusCode, body)
	}
	values.Set("_token", browserCSRF(t, body))
	values.Set("intent", "save")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+path, values)
	if response.StatusCode != http.StatusConflict || !strings.Contains(body, "was changed") {
		t.Fatalf("stale corrected save: got %d: %s", response.StatusCode, body)
	}

	values.Set("_token", browserCSRF(t, body))
	values.Set("title", "")
	values.Set("version", "not-a-version")
	values.Set("intent", "save")
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+path, values)
	if response.StatusCode != http.StatusUnprocessableEntity || browserVersion(t, body) != current.Version {
		t.Fatalf("invalid version did not recover authorized current version %d: got %d: %s", current.Version, response.StatusCode, body)
	}
}

func testScalableOwnerAndPrivilegedChoices(t *testing.T, owner, foreign, admin *http.Client, baseURL string, seed scalableSeed, ticket scalableTicket) {
	t.Helper()
	ownerPath := fmt.Sprintf("/app/tickets/%d/edit", ticket.ID)
	response, ownerBody := requestBrowser(t, owner, http.MethodGet, baseURL+ownerPath, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("owner edit form: got %d: %s", response.StatusCode, ownerBody)
	}
	if strings.Contains(ownerBody, "Foreign private category") || strings.Contains(ownerBody, "PROJECT_PRIVATE_") {
		t.Fatalf("owner form disclosed foreign choice or ID-only project text: %s", ownerBody)
	}
	for _, unavailable := range []int64{seed.ForeignCategory, seed.DeletedCategory, 9_223_372_036_854_775_000} {
		response, formBody := requestBrowser(t, owner, http.MethodGet, baseURL+"/app/tickets/new", nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("open form for unavailable selection %d: got %d: %s", unavailable, response.StatusCode, formBody)
		}
		values := scalableTicketForm(browserCSRF(t, formBody), seed)
		values.Set("category_id", strconv.FormatInt(unavailable, 10))
		values.Set("choice[category][id]", strconv.FormatInt(unavailable, 10))
		values.Set("intent", "choice:category:lookup")
		response, formBody = requestBrowser(t, owner, http.MethodPost, baseURL+"/app/tickets", values)
		if response.StatusCode != http.StatusOK || !strings.Contains(formBody, "Unavailable selection") ||
			strings.Contains(formBody, "Foreign private category") || strings.Contains(formBody, "Deleted private category") {
			t.Fatalf("unavailable selection %d disclosed existence or label: got %d: %s", unavailable, response.StatusCode, formBody)
		}
	}
	// ID-only is a source-form disclosure boundary, not a restriction on an
	// independently authorized Project resource index. The owner may search
	// their own declared Project text field without exposing another owner.
	response, body := requestJSON(t, owner, http.MethodGet, baseURL+"/projects?q=PROJECT_PRIVATE&per_page=100", "")
	var projects struct {
		Data []struct {
			PrivateLabel string `json:"private_label"`
		} `json:"data"`
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authorized target resource search: got %d: %s", response.StatusCode, body)
	}
	if err := json.Unmarshal([]byte(body), &projects); err != nil {
		t.Fatalf("decode authorized target resource search: %v: %s", err, body)
	}
	labels := make(map[string]bool, len(projects.Data))
	for _, project := range projects.Data {
		labels[project.PrivateLabel] = true
	}
	if len(projects.Data) != 30 || labels["FOREIGN_PROJECT_PRIVATE"] || labels["ADMIN_PROJECT_PRIVATE"] {
		t.Fatalf("authorized target resource search crossed its owner boundary: rows=%d labels=%v", len(projects.Data), labels)
	}
	for index := 1; index <= 30; index++ {
		label := fmt.Sprintf("PROJECT_PRIVATE_%02d", index)
		if !labels[label] {
			t.Errorf("authorized target resource search omitted %q", label)
		}
	}
	response, hiddenBody := requestBrowser(t, foreign, http.MethodGet, baseURL+ownerPath, nil)
	responseMissing, missingBody := requestBrowser(t, foreign, http.MethodGet, baseURL+"/app/tickets/9223372036854775000/edit", nil)
	if response.StatusCode != http.StatusNotFound || responseMissing.StatusCode != http.StatusNotFound || hiddenBody != missingBody {
		t.Fatalf("foreign and missing edits were distinguishable: foreign=%d %q missing=%d %q", response.StatusCode, hiddenBody, responseMissing.StatusCode, missingBody)
	}
	response, adminBody := requestBrowser(t, admin, http.MethodGet, baseURL+ownerPath, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("privileged edit form: got %d: %s", response.StatusCode, adminBody)
	}
	response, body = requestJSON(t, admin, http.MethodGet, baseURL+"/categories?q=Middle+searchable", "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("privileged source editor unexpectedly had target Category list access: got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, admin, http.MethodGet, baseURL+"/projects?id="+strconv.FormatInt(seed.OwnerProject, 10), "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("privileged source editor unexpectedly had target Project list access: got %d: %s", response.StatusCode, body)
	}
	assertScalableChoice(t, adminBody, "category_id", seed.MiddleCategory, "Middle searchable category", true)
	assertScalableChoice(t, adminBody, "project_id", seed.OwnerProject, "", true)
	if strings.Contains(adminBody, "PROJECT_PRIVATE_") || strings.Contains(adminBody, "Admin private category") || strings.Contains(adminBody, "ADMIN_PROJECT_PRIVATE") {
		t.Fatalf("privileged form used actor choices or disclosed ID-only target text: %s", adminBody)
	}
}

func testScalableListParity(t *testing.T, owner, foreign *http.Client, baseURL string, seed scalableSeed, database *sql.DB) {
	t.Helper()
	notesNeedle := "needle appears only in notes"
	first := createScalableTicket(t, owner, baseURL, scalableTicketJSON("needle appears in title", nil, 1, nil, true, nil, seed.FirstCategory, seed.OwnerProject, nil))
	estimate := int64(2)
	featured := false
	second := createScalableTicket(t, owner, baseURL, scalableTicketJSON("unrelated title", &notesNeedle, 2, &estimate, false, &featured, seed.FirstCategory, seed.OwnerProject, nil))
	otherEstimate := int64(5)
	third := createScalableTicket(t, owner, baseURL, scalableTicketJSON("needle wrong category", nil, 3, &otherEstimate, true, nil, seed.MiddleCategory, seed.OwnerProject, nil))
	foreignTicket := createScalableTicket(t, foreign, baseURL, scalableTicketJSON("needle foreign owner", nil, 4, nil, true, nil, seed.ForeignCategory, seed.ForeignProject, nil))
	injection := `literal %_!\ ' OR 1=1 -- probe`
	injectionTicket := createScalableTicket(t, owner, baseURL, scalableTicketJSON(injection, nil, 5, nil, true, nil, seed.FirstCategory, seed.OwnerProject, nil))

	baseQuery := url.Values{
		"q": {"needle"}, "category_id": {strconv.FormatInt(seed.FirstCategory, 10)},
		"sort": {"estimate"}, "direction": {"asc"}, "page": {"1"}, "per_page": {"100"},
	}
	page, browserIDs, _ := scalableListPair(t, owner, baseURL, baseQuery)
	if !slices.Equal(browserIDs, []int64{second.ID, first.ID}) || !slices.Equal(scalableTicketIDs(page.Data), browserIDs) {
		t.Fatalf("ASC nullable browser/JSON parity = browser:%v JSON:%v, want [%d %d] (excluded category=%d foreign=%d)", browserIDs, scalableTicketIDs(page.Data), second.ID, first.ID, third.ID, foreignTicket.ID)
	}
	if page.Pagination.Page != 1 || page.Pagination.PerPage != 100 || page.Pagination.HasPrevious || page.Pagination.HasNext {
		t.Fatalf("JSON pagination metadata = %+v", page.Pagination)
	}
	baseQuery.Set("direction", "desc")
	page, browserIDs, _ = scalableListPair(t, owner, baseURL, baseQuery)
	if !slices.Equal(browserIDs, []int64{first.ID, second.ID}) || !slices.Equal(scalableTicketIDs(page.Data), browserIDs) {
		t.Fatalf("DESC nullable browser/JSON parity = browser:%v JSON:%v, want [%d %d]", browserIDs, scalableTicketIDs(page.Data), first.ID, second.ID)
	}

	injectionQuery := url.Values{"q": {injection}, "sort": {"id"}, "direction": {"asc"}, "page": {"1"}, "per_page": {"20"}}
	page, browserIDs, _ = scalableListPair(t, owner, baseURL, injectionQuery)
	if !slices.Equal(browserIDs, []int64{injectionTicket.ID}) || !slices.Equal(scalableTicketIDs(page.Data), browserIDs) {
		t.Fatalf("literal wildcard/injection search = browser:%v JSON:%v want [%d]", browserIDs, scalableTicketIDs(page.Data), injectionTicket.ID)
	}

	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO tickets (user_id, category_id, project_id, title, priority, active)
		SELECT $1, $2, $3, 'paging marker ' || value::text, value, true
		FROM generate_series(1, 25) AS value`, seed.OwnerID, seed.FirstCategory, seed.OwnerProject); err != nil {
		t.Fatalf("seed paginated ticket index: %v", err)
	}
	pagingQuery := url.Values{
		"q": {"paging marker"}, "category_id": {strconv.FormatInt(seed.FirstCategory, 10)},
		"sort": {"id"}, "direction": {"asc"}, "page": {"1"}, "per_page": {"20"},
	}
	page, browserIDs, browserBody := scalableListPair(t, owner, baseURL, pagingQuery)
	if len(page.Data) != 20 || len(browserIDs) != 20 || !page.Pagination.HasNext || !slices.Equal(scalableTicketIDs(page.Data), browserIDs) {
		t.Fatalf("bounded paginated list = browser:%v JSON:%v metadata:%+v", browserIDs, scalableTicketIDs(page.Data), page.Pagination)
	}
	nextQuery := url.Values{}
	for name, values := range pagingQuery {
		nextQuery[name] = slices.Clone(values)
	}
	nextQuery.Set("page", "2")
	if !strings.Contains(stdhtml.UnescapeString(browserBody), `/app/tickets?`+nextQuery.Encode()) {
		t.Fatalf("browser Next link did not retain every filter: want %s in %s", nextQuery.Encode(), browserBody)
	}
}

func testScalableInputRejection(t *testing.T, client *http.Client, baseURL string, seed scalableSeed, database *sql.DB) {
	t.Helper()
	for _, target := range []string{
		baseURL + "/tickets?q=%00",
		baseURL + "/app/tickets?q=%00",
		baseURL + "/tickets?q=ok&q=duplicate",
		baseURL + "/app/tickets?sort=unknown",
		baseURL + "/tickets?page=10001",
		baseURL + "/app/tickets?per_page=101",
	} {
		response, body := requestBrowser(t, client, http.MethodGet, target, nil)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid scalable query %q: got %d: %s", target, response.StatusCode, body)
		}
	}
	response, body := requestBrowser(t, client, http.MethodGet, baseURL+"/app/tickets/new", nil)
	values := scalableTicketForm(browserCSRF(t, body), seed)
	values.Set("choice[category][q]", "bad\x00choice")
	values.Set("intent", "choice:category:search")
	before := scalableTicketCount(t, context.Background(), database)
	response, body = requestBrowser(t, client, http.MethodPost, baseURL+"/app/tickets", values)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("NUL choice search: got %d: %s", response.StatusCode, body)
	}
	if after := scalableTicketCount(t, context.Background(), database); after != before {
		t.Fatalf("invalid choice search wrote tickets: before=%d after=%d", before, after)
	}
}

func scalableTicketForm(csrf string, seed scalableSeed) url.Values {
	return url.Values{
		"_token": {csrf}, "intent": {"save"}, "validation_attempted": {"0"},
		"title": {"Scalable browser ticket"}, "notes": {""}, "priority": {"0"}, "estimate": {""},
		"active": {"false"}, "featured": {""},
		"category_id": {strconv.FormatInt(seed.FirstCategory, 10)}, "project_id": {strconv.FormatInt(seed.OwnerProject, 10)},
		"choice[category][q]": {""}, "choice[category][id]": {""}, "choice[category][page]": {"1"},
		"choice[project][id]": {""}, "choice[project][page]": {"1"},
	}
}

func scalableTicketJSON(title string, notes *string, priority int64, estimate *int64, active bool, featured *bool, categoryID, projectID int64, version *int64) string {
	payload := map[string]any{
		"title": title, "notes": notes, "priority": priority, "estimate": estimate,
		"active": active, "featured": featured, "category_id": categoryID, "project_id": projectID,
	}
	if version != nil {
		payload["version"] = *version
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func createScalableTicket(t *testing.T, client *http.Client, baseURL, payload string) scalableTicket {
	t.Helper()
	response, body := requestJSON(t, client, http.MethodPost, baseURL+"/tickets", payload)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create scalable ticket: got %d: %s", response.StatusCode, body)
	}
	return decodeScalableTicket(t, body)
}

func decodeScalableTicket(t *testing.T, body string) scalableTicket {
	t.Helper()
	var envelope struct {
		Data scalableTicket `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil || envelope.Data.ID < 1 {
		t.Fatalf("decode scalable ticket: %v: %s", err, body)
	}
	return envelope.Data
}

func scalableListPair(t *testing.T, client *http.Client, baseURL string, query url.Values) (scalableTicketPage, []int64, string) {
	t.Helper()
	response, body := requestJSON(t, client, http.MethodGet, baseURL+"/tickets?"+query.Encode(), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("JSON scalable list %q: got %d: %s", query.Encode(), response.StatusCode, body)
	}
	var page scalableTicketPage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode scalable list: %v: %s", err, body)
	}
	response, body = requestBrowser(t, client, http.MethodGet, baseURL+"/app/tickets?"+query.Encode(), nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("browser scalable list %q: got %d: %s", query.Encode(), response.StatusCode, body)
	}
	return page, scalableBrowserTicketIDs(t, body), body
}

func scalableTicketIDs(tickets []scalableTicket) []int64 {
	ids := make([]int64, len(tickets))
	for index, ticket := range tickets {
		ids[index] = ticket.ID
	}
	return ids
}

var scalableTicketLinkPattern = regexp.MustCompile(`href="/app/tickets/([0-9]+)"`)

func scalableBrowserTicketIDs(t *testing.T, body string) []int64 {
	t.Helper()
	matches := scalableTicketLinkPattern.FindAllStringSubmatch(body, -1)
	ids := make([]int64, 0, len(matches))
	for _, match := range matches {
		id, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func assertScalableChoice(t *testing.T, body, field string, id int64, labelContains string, selected bool) string {
	t.Helper()
	selectPattern := regexp.MustCompile(`(?s)<select[^>]*name="` + regexp.QuoteMeta(field) + `"[^>]*>(.*?)</select>`)
	selectMatch := selectPattern.FindStringSubmatch(body)
	if len(selectMatch) != 2 {
		t.Fatalf("choice field %q is absent: %s", field, body)
	}
	pattern := regexp.MustCompile(fmt.Sprintf(`<option[^>]*value="%d"([^>]*)>([^<]*(?:&[^;]+;[^<]*)*)</option>`, id))
	match := pattern.FindStringSubmatch(selectMatch[1])
	if len(match) != 3 {
		t.Fatalf("choice %d is absent from %q: %s", id, field, selectMatch[0])
	}
	label := stdhtml.UnescapeString(match[2])
	if labelContains != "" && !strings.Contains(label, labelContains) {
		t.Fatalf("choice %d in %q label %q does not contain %q", id, field, label, labelContains)
	}
	if !strings.Contains(label, strconv.FormatInt(id, 10)) {
		t.Fatalf("choice %d in %q label does not retain its ID: %q", id, field, label)
	}
	if selected != strings.Contains(match[1], "selected") {
		t.Fatalf("choice %d in %q selected=%t, want %t: %s", id, field, strings.Contains(match[1], "selected"), selected, match[0])
	}
	return label
}

func scalableInputValue(t *testing.T, body, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`<input[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*value="([^"]*)"[^>]*>`)
	match := pattern.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("form input %q is absent: %s", name, body)
	}
	return stdhtml.UnescapeString(match[1])
}

func scalableTextareaValue(t *testing.T, body, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?s)<textarea[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>(.*?)</textarea>`)
	match := pattern.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("form textarea %q is absent: %s", name, body)
	}
	value := strings.ReplaceAll(strings.ReplaceAll(match[1], "\r\n", "\n"), "\r", "\n")
	return stdhtml.UnescapeString(strings.TrimPrefix(value, "\n"))
}

func TestScalableTextareaValueModelsNativeHTMLParsing(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "empty", body: "<textarea name=\"notes\">\n</textarea>", want: ""},
		{name: "padded", body: "<textarea name=\"notes\">\n  padded  \n</textarea>", want: "  padded  \n"},
		{name: "leading LF", body: "<textarea name=\"notes\">\r\n\r\nleading\rline</textarea>", want: "\nleading\nline"},
		{name: "entities", body: "<textarea name=\"notes\">\n&lt;b&gt;&amp;</textarea>", want: "<b>&"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := scalableTextareaValue(t, test.body, "notes"); got != test.want {
				t.Fatalf("textarea value = %q, want %q", got, test.want)
			}
		})
	}
}

func scalableSelectedOption(t *testing.T, body, name string) string {
	t.Helper()
	selectPattern := regexp.MustCompile(`(?s)<select[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>(.*?)</select>`)
	selectedPattern := regexp.MustCompile(`<option[^>]*value="([^"]*)"[^>]*selected[^>]*>`)
	selection := selectPattern.FindStringSubmatch(body)
	if len(selection) != 2 {
		t.Fatalf("form select %q is absent: %s", name, body)
	}
	match := selectedPattern.FindStringSubmatch(selection[1])
	if len(match) != 2 {
		t.Fatalf("form select %q has no selected option: %s", name, selection[1])
	}
	return stdhtml.UnescapeString(match[1])
}

func scalableLocationID(t *testing.T, location, prefix string) int64 {
	t.Helper()
	if !strings.HasPrefix(location, prefix) {
		t.Fatalf("redirect location %q does not begin with %q", location, prefix)
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(location, prefix), 10, 64)
	if err != nil || id < 1 {
		t.Fatalf("redirect location %q has invalid ID: %v", location, err)
	}
	return id
}

func scalableTicketCount(t *testing.T, ctx context.Context, database *sql.DB) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM tickets").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func customizeScalableTicketAuthorization(t *testing.T, directory string) {
	t.Helper()
	path := filepath.Join(directory, "internal", "resources", "ticket", "authorization.go")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	start := strings.Index(source, "func Authorize(")
	endMarker := "\n}\n\n// Scope"
	end := strings.Index(source[start:], endMarker)
	if start < 0 || end < 0 || strings.Count(source, "func Authorize(") != 1 {
		t.Fatalf("generated Ticket authorization policy had an unexpected shape: %s", source)
	}
	end += start + len("\n}")
	custom := `func Authorize(_ context.Context, user auth.User, action Action) (Access, error) {
	switch action {
	case ActionList, ActionCreate, ActionView, ActionUpdate, ActionDelete:
		if user.Email == "admin@example.com" {
			return AccessAll, nil
		}
		return AccessOwner, nil
	default:
		return AccessDenied, nil
	}
}`
	if err := os.WriteFile(path, []byte(source[:start]+custom+source[end:]), 0o644); err != nil {
		t.Fatal(err)
	}
}

func customizeScalableTargetListAuthorization(t *testing.T, directory, packageName string) {
	t.Helper()
	path := filepath.Join(directory, "internal", "resources", packageName, "authorization.go")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	start := strings.Index(source, "func Authorize(")
	endMarker := "\n}\n\n// Scope"
	end := strings.Index(source[start:], endMarker)
	if start < 0 || end < 0 || strings.Count(source, "func Authorize(") != 1 {
		t.Fatalf("generated %s authorization policy had an unexpected shape: %s", packageName, source)
	}
	end += start + len("\n}")
	custom := `func Authorize(_ context.Context, user auth.User, action Action) (Access, error) {
	if user.Email == "admin@example.com" && action == ActionList {
		return AccessDenied, nil
	}
	switch action {
	case ActionList, ActionCreate, ActionView, ActionUpdate, ActionDelete:
		return AccessOwner, nil
	default:
		return AccessDenied, nil
	}
}`
	if err := os.WriteFile(path, []byte(source[:start]+custom+source[end:]), 0o644); err != nil {
		t.Fatal(err)
	}
}

const scalableResourcesAcceptanceProgram = `package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	ticket "example.com/scalable/internal/resources/ticket"
	"github.com/ShanilKoshitha/goforge/orm"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type observedStatement struct {
	event orm.StatementEvent
	sql string
	args []any
}

func main() {
	if len(os.Args) != 6 {
		panic("usage: scalable_resources_acceptance <owner> <first-category> <last-category> <first-project> <last-project>")
	}
	values := make([]int64, 5)
	for index, raw := range os.Args[1:] {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 { panic("invalid positive acceptance ID") }
		values[index] = value
	}
	database, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil { panic(err) }
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil { panic(err) }
	proveRepository(ctx, database, values)
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil { panic(err) }
	proveRepository(ctx, transaction, values)
	if err := transaction.Rollback(); err != nil { panic(err) }
	fmt.Println("scalable resources acceptance passed")
}

func proveRepository(ctx context.Context, executor orm.Executor, values []int64) {
	ownerID, firstCategory, lastCategory, firstProject, lastProject := values[0], values[1], values[2], values[3], values[4]
	statements := make([]observedStatement, 0, 12)
	observed := orm.ObserveExecutor(executor, func(_ context.Context, event orm.StatementEvent, statement orm.Statement) {
		statements = append(statements, observedStatement{event: event, sql: statement.SQL(), args: statement.Args()})
	})
	repository := ticket.NewPostgresRepository(observed)

	before := len(statements)
	options := ticket.DefaultListOptions()
	options.Search = "needle"
	options.CategoryID = firstCategory
	options.Sort = "estimate"
	options.Direction = "asc"
	options.PerPage = 100
	page, err := repository.Browse(ctx, ticket.OwnerScope(ownerID), options)
	if err != nil || len(page.Items) != 2 || page.HasNext { panic("bounded browse through observed executor") }
	queries := queryStatements(statements[before:])
	if len(queries) != 3 { panic(fmt.Sprintf("browse query budget = %d, want 3", len(queries))) }
	primary := queries[0]
	for _, fragment := range []string{"user_id", "category_id", "ILIKE", "ESCAPE '!'", "ORDER BY", "estimate", "ASC", "id", "LIMIT", "OFFSET"} {
		if !strings.Contains(primary.sql, fragment) { panic("browse SQL omits " + fragment + ": " + primary.sql) }
	}
	if !hasArgument(primary.args, ownerID) || !hasArgument(primary.args, firstCategory) || !hasArgument(primary.args, "%needle%") || !hasArgument(primary.args, int64(101)) {
		panic(fmt.Sprintf("browse arguments do not expose scope/filter/sentinel: %#v", primary.args))
	}

	before = len(statements)
	categoryOptions := ticket.DefaultChoiceOptions()
	categoryOptions.Search = "Last searchable"
	categoryOptions.SelectedID = firstCategory
	categoryPage, err := repository.CategoryChoices(ctx, ownerID, categoryOptions)
	if err != nil || len(categoryPage.Items) != 1 || categoryPage.Items[0].ID != lastCategory || categoryPage.Selected == nil || categoryPage.Selected.ID != firstCategory {
		panic("bounded category choices and selected lookup")
	}
	queries = queryStatements(statements[before:])
	if len(queries) != 2 { panic(fmt.Sprintf("category choice query budget = %d, want 2", len(queries))) }
	for _, fragment := range []string{"user_id", "ILIKE", "ESCAPE '!'", "ORDER BY", "id", "ASC", "LIMIT", "OFFSET"} {
		if !strings.Contains(queries[0].sql, fragment) { panic("category choice SQL omits " + fragment + ": " + queries[0].sql) }
	}
	if !hasArgument(queries[0].args, ownerID) || !hasArgument(queries[0].args, "%Last searchable%") || !hasArgument(queries[0].args, int64(21)) {
		panic(fmt.Sprintf("category choice arguments do not expose scope/search/sentinel: %#v", queries[0].args))
	}
	if !hasArgument(queries[1].args, ownerID) || !hasArgument(queries[1].args, firstCategory) {
		panic(fmt.Sprintf("selected category lookup is not owner-scoped and exact: %#v", queries[1].args))
	}

	before = len(statements)
	projectOptions := ticket.DefaultChoiceOptions()
	projectOptions.ID = lastProject
	projectOptions.SelectedID = firstProject
	projectPage, err := repository.ProjectChoices(ctx, ownerID, projectOptions)
	if err != nil || len(projectPage.Items) != 1 || projectPage.Items[0].ID != lastProject || projectPage.Selected == nil || projectPage.Selected.ID != firstProject {
		panic("ID-only project choices and selected lookup")
	}
	queries = queryStatements(statements[before:])
	if len(queries) != 2 { panic(fmt.Sprintf("project choice query budget = %d, want 2", len(queries))) }
	if strings.Contains(queries[0].sql, "ILIKE") || !hasArgument(queries[0].args, ownerID) || !hasArgument(queries[0].args, lastProject) {
		panic("ID-only project lookup widened into text or lost owner/ID scope")
	}
}

func queryStatements(statements []observedStatement) []observedStatement {
	queries := make([]observedStatement, 0, len(statements))
	for _, statement := range statements {
		if statement.event == orm.StatementQuery { queries = append(queries, statement) }
	}
	return queries
}

func hasArgument(arguments []any, expected any) bool {
	want := fmt.Sprint(expected)
	for _, argument := range arguments {
		if fmt.Sprint(argument) == want { return true }
	}
	return false
}
`
