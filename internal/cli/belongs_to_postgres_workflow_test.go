package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGeneratedBelongsToPostgresWorkflow(t *testing.T) {
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

	directory := filepath.Join(scratch, "relations")
	if output, err := generatedCommand(scratch, baseEnvironment, forgeBinary, "new", "relations", "--module", "example.com/issueboard", "--replace", root); err != nil {
		t.Fatalf("forge new: %v\n%s", err, output)
	}
	commands := [][]string{
		{"make:resource", "Category", "--field", "title:string"},
		{"make:resource", "AlternateCategory", "--field", "title:string"},
		{"make:resource", "Issue", "--field", "summary:string", "--belongs-to", "category:Category"},
	}
	for _, arguments := range commands {
		if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, arguments...); err != nil {
			t.Fatalf("forge %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, ".forge", "belongs_to_acceptance.go"), []byte(belongsToAcceptanceProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := generatedCommand(directory, baseEnvironment, "go", "test", "./..."); err != nil {
		t.Fatalf("generated relationship application tests: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, "go", "build", "./.forge/belongs_to_acceptance.go"); err != nil {
		t.Fatalf("generated relationship acceptance helper build: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "build"); err != nil {
		t.Fatalf("build generated application: %v\n%s", err, output)
	}

	schema := fmt.Sprintf("goforge_belongs_to_%d", time.Now().UnixNano())
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
	environment := append(baseEnvironment, "DATABASE_URL="+isolatedURL, "APP_ENV=local")
	if output, err := generatedCommand(directory, environment, forgeBinary, "migrate"); err != nil {
		t.Fatalf("migrate relationship application: %v\n%s", err, output)
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

	stamp := time.Now().UnixNano()
	firstEmail := fmt.Sprintf("owner-%d@example.com", stamp)
	secondEmail := fmt.Sprintf("other-%d@example.com", stamp)
	first := clientWithCookies(t)
	second := clientWithCookies(t)
	registerBelongsToUser(t, first, baseURL, "Owner", firstEmail)
	registerBelongsToUser(t, second, baseURL, "Other", secondEmail)

	categoryID, _, _ := createBelongsToResource(t, first, baseURL+"/categories", `{"title":"Owned category"}`)
	replacementID, _, _ := createBelongsToResource(t, first, baseURL+"/categories", `{"title":"Replacement category"}`)
	crossOwnerID, _, _ := createBelongsToResource(t, second, baseURL+"/categories", `{"title":"Foreign category"}`)

	missingBody := expectInvalidBelongsTo(t, first, baseURL, 9_223_372_036_854_775_000)
	crossOwnerBody := expectInvalidBelongsTo(t, first, baseURL, crossOwnerID)
	if missingBody != crossOwnerBody {
		t.Fatalf("missing and cross-owner associations were distinguishable: missing=%s cross-owner=%s", missingBody, crossOwnerBody)
	}

	issueID, version, body := createBelongsToResource(t, first, baseURL+"/issues",
		fmt.Sprintf(`{"summary":"Owned issue","category_id":%d}`, categoryID))
	assertFlatRelationshipJSON(t, body, categoryID)
	issueURL := fmt.Sprintf("%s/issues/%d", baseURL, issueID)
	response, body := requestJSON(t, first, http.MethodGet, issueURL, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("read owned issue: got %d: %s", response.StatusCode, body)
	}
	assertFlatRelationshipJSON(t, body, categoryID)
	response, body = requestJSON(t, first, http.MethodPut, issueURL,
		fmt.Sprintf(`{"summary":"Updated issue","category_id":%d,"version":%d}`, replacementID, version))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("update owned issue: got %d: %s", response.StatusCode, body)
	}
	assertFlatRelationshipJSON(t, body, replacementID)
	_, version = decodeBelongsToResource(t, body)
	response, body = requestJSON(t, first, http.MethodPut, issueURL,
		fmt.Sprintf(`{"summary":"Stale issue","category_id":%d,"version":1}`, replacementID))
	if response.StatusCode != http.StatusConflict || !strings.Contains(body, "was changed") {
		t.Fatalf("stale relationship update: got %d: %s", response.StatusCode, body)
	}

	response, body = requestJSON(t, second, http.MethodGet, issueURL, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner relationship read: got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, second, http.MethodGet, baseURL+"/issues", "")
	if response.StatusCode != http.StatusOK || strings.Contains(body, "Updated issue") {
		t.Fatalf("cross-owner relationship list leaked data: got %d: %s", response.StatusCode, body)
	}

	response, body = requestBrowser(t, first, http.MethodGet, baseURL+"/app/issues/new", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, fmt.Sprintf("Category #%d", categoryID)) ||
		strings.Contains(body, fmt.Sprintf("Category #%d", crossOwnerID)) {
		t.Fatalf("browser choices were not owner-only: got %d: %s", response.StatusCode, body)
	}
	csrf := browserCSRF(t, body)
	response, body = requestBrowser(t, first, http.MethodPost, baseURL+"/app/issues", url.Values{
		"_token": {csrf}, "summary": {"Rejected browser issue"}, "category_id": {strconv.FormatInt(crossOwnerID, 10)},
	})
	selected := fmt.Sprintf(`value="%d" selected`, crossOwnerID)
	if response.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "is not available") ||
		!strings.Contains(body, "Unavailable selection") || !strings.Contains(body, selected) {
		t.Fatalf("invalid browser selection was not safely retained: got %d: %s", response.StatusCode, body)
	}

	response, body = requestJSON(t, first, http.MethodDelete, baseURL+"/categories/"+strconv.FormatInt(replacementID, 10), "")
	if response.StatusCode != http.StatusConflict || !strings.Contains(body, "still in use") {
		t.Fatalf("referenced parent delete: got %d: %s", response.StatusCode, body)
	}
	response, body = requestJSON(t, first, http.MethodDelete, baseURL+"/categories/"+strconv.FormatInt(categoryID, 10), "")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("unreferenced parent delete: got %d: %s", response.StatusCode, body)
	}

	if output, err := generatedCommand(directory, environment, "go", "run", "./.forge/belongs_to_acceptance.go",
		firstEmail, secondEmail, strconv.FormatInt(replacementID, 10), strconv.FormatInt(issueID, 10)); err != nil {
		t.Fatalf("real PostgreSQL relationship proof: %v\n%s", err, output)
	} else if strings.TrimSpace(output) != "belongs-to acceptance passed" {
		t.Fatalf("unexpected relationship proof output: %s", output)
	}
}

func registerBelongsToUser(t *testing.T, client *http.Client, baseURL, name, email string) {
	t.Helper()
	response, body := requestJSON(t, client, http.MethodPost, baseURL+"/auth/register",
		fmt.Sprintf(`{"name":%q,"email":%q,"password":"a secure passphrase","password_confirmation":"a secure passphrase"}`, name, email))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register relationship user: got %d: %s", response.StatusCode, body)
	}
}

func createBelongsToResource(t *testing.T, client *http.Client, target, payload string) (int64, int64, string) {
	t.Helper()
	response, body := requestJSON(t, client, http.MethodPost, target, payload)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create relationship resource: got %d: %s", response.StatusCode, body)
	}
	id, version := decodeBelongsToResource(t, body)
	return id, version, body
}

func decodeBelongsToResource(t *testing.T, body string) (int64, int64) {
	t.Helper()
	var envelope struct {
		Data struct {
			ID      int64 `json:"id"`
			Version int64 `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil || envelope.Data.ID < 1 || envelope.Data.Version < 1 {
		t.Fatalf("decode relationship resource: %v: %s", err, body)
	}
	return envelope.Data.ID, envelope.Data.Version
}

func expectInvalidBelongsTo(t *testing.T, client *http.Client, baseURL string, categoryID int64) string {
	t.Helper()
	response, body := requestJSON(t, client, http.MethodPost, baseURL+"/issues",
		fmt.Sprintf(`{"summary":"Rejected issue","category_id":%d}`, categoryID))
	if response.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, `"path":"category_id"`) ||
		!strings.Contains(body, `"code":"association.invalid"`) || strings.Contains(body, strconv.FormatInt(categoryID, 10)) {
		t.Fatalf("invalid association response disclosed or omitted detail: got %d: %s", response.StatusCode, body)
	}
	return body
}

func assertFlatRelationshipJSON(t *testing.T, body string, categoryID int64) {
	t.Helper()
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode relationship JSON: %v: %s", err, body)
	}
	var got int64
	if err := json.Unmarshal(envelope.Data["category_id"], &got); err != nil || got != categoryID {
		t.Fatalf("relationship JSON category_id = %d, %v: %s", got, err, body)
	}
	if _, exists := envelope.Data["category"]; exists {
		t.Fatalf("relationship JSON nested category: %s", body)
	}
}

const belongsToAcceptanceProgram = `package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	category "example.com/issueboard/internal/resources/category"
	issue "example.com/issueboard/internal/resources/issue"
	"github.com/ShanilKoshitha/goforge/orm"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	if len(os.Args) != 5 {
		panic("acceptance arguments are required")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		panic("open database")
	}
	defer db.Close()
	firstUser := userID(ctx, db, os.Args[1])
	secondUser := userID(ctx, db, os.Args[2])
	categoryID := positiveID(os.Args[3])
	issueID := positiveID(os.Args[4])

	if _, err := db.ExecContext(ctx,
		"INSERT INTO issues (user_id, category_id, summary) VALUES ($1, $2, $3)",
		secondUser, categoryID, "raw cross-owner"); err == nil {
		panic("raw cross-owner insert succeeded")
	} else {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
			panic("raw cross-owner insert did not return SQLSTATE 23503")
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		panic("begin repository transaction")
	}
	transactionRepository := issue.NewPostgresRepository(tx)
	created, err := transactionRepository.Create(ctx, firstUser,
		issue.Attributes{Summary: "rolled back issue"}, issue.AssociationIDs{CategoryID: categoryID})
	if err != nil || created.ID < 1 {
		_ = tx.Rollback()
		panic("create through explicit repository transaction")
	}
	if err := tx.Rollback(); err != nil {
		panic("rollback repository transaction")
	}
	var rolledBack int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM issues WHERE summary = $1", "rolled back issue").Scan(&rolledBack); err != nil || rolledBack != 0 {
		panic("repository transaction rollback was not effective")
	}

	type observedStatement struct {
		event orm.StatementEvent
		sql   string
	}
	var observed []observedStatement
	executor := orm.ObserveExecutor(db, func(_ context.Context, event orm.StatementEvent, statement orm.Statement) {
		observed = append(observed, observedStatement{event: event, sql: statement.SQL()})
	})
	repository := issue.NewPostgresRepository(executor)
	checkQueries := func(label string, before, want int) {
		queries := 0
		for _, statement := range observed[before:] {
			if statement.event == orm.StatementQuery {
				queries++
			}
		}
		if queries != want {
			panic(label + " query budget")
		}
	}
	before := len(observed)
	if items, err := repository.List(ctx, firstUser); err != nil || len(items) != 1 || items[0].Category == nil || items[0].Category.ID != categoryID {
		panic("bounded eager list")
	}
	checkQueries("list", before, 2)
	if !strings.Contains(observed[before].sql, "LIMIT") {
		panic("list query is unbounded")
	}
	before = len(observed)
	if page, err := repository.Paginate(ctx, firstUser, 1, 20); err != nil || len(page.Items) != 1 || page.Items[0].Category == nil {
		panic("bounded eager pagination")
	}
	checkQueries("paginate", before, 2)
	before = len(observed)
	if item, err := repository.Find(ctx, firstUser, issueID); err != nil || item.Category == nil || item.Category.ID != categoryID {
		panic("bounded eager find")
	}
	checkQueries("find", before, 2)
	before = len(observed)
	choices, err := repository.CategoryChoices(ctx, firstUser)
	if err != nil || len(choices) != 1 || choices[0].ID != categoryID {
		panic("owner-scoped choices")
	}
	checkQueries("choices", before, 1)
	choiceSQL := observed[before].sql
	if !strings.Contains(choiceSQL, "LIMIT") || !strings.Contains(choiceSQL, "user_id") {
		panic("choice query is not bounded and owner-scoped")
	}

	var raceCategoryID int64
	if err := db.QueryRowContext(ctx,
		"INSERT INTO categories (user_id, title) VALUES ($1, $2) RETURNING id", firstUser, "race category").Scan(&raceCategoryID); err != nil {
		panic("seed race category")
	}
	categoryRepository := category.NewPostgresRepository(db)
	issueRepository := issue.NewPostgresRepository(db)
	start := make(chan struct{})
	var group sync.WaitGroup
	var createErr, deleteErr error
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		_, createErr = issueRepository.Create(ctx, firstUser,
			issue.Attributes{Summary: "race issue"}, issue.AssociationIDs{CategoryID: raceCategoryID})
	}()
	go func() {
		defer group.Done()
		<-start
		deleteErr = categoryRepository.Delete(ctx, firstUser, raceCategoryID)
	}()
	close(start)
	group.Wait()
	var invalid *issue.InvalidAssociationError
	createWon := createErr == nil && errors.Is(deleteErr, category.ErrInUse)
	deleteWon := deleteErr == nil && errors.As(createErr, &invalid) && invalid.Field == "category_id"
	if !createWon && !deleteWon {
		panic("parent-delete/create race escaped its two legal outcomes")
	}

	fmt.Println("belongs-to acceptance passed")
}

func userID(ctx context.Context, db *sql.DB, email string) int64 {
	var id int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM users WHERE email = $1", email).Scan(&id); err != nil || id < 1 {
		panic("lookup acceptance user")
	}
	return id
}

func positiveID(value string) int64 {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		panic("invalid acceptance ID")
	}
	return id
}

`
