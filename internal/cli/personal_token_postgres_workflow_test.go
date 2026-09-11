package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	securitytoken "github.com/ShanilKoshitha/goforge/security/token"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const personalTokenAcceptancePrefix = "goforge_pat_"

var personalTokenAcceptancePattern = regexp.MustCompile(`goforge_pat_[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`)

type personalTokenAcceptanceRecord struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
	Token     string `json:"token,omitempty"`
}

type personalTokenAcceptanceMaterial struct {
	Raw      string
	Selector string
	Secret   string
}

type personalTokenAcceptanceResponse struct {
	Status int
	Header http.Header
	Body   string
}

type personalTokenAcceptanceResource struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	CategoryID int64  `json:"category_id"`
	Version    int64  `json:"version"`
}

func TestPersonalTokenAcceptanceEnvironmentRetainsRequiredSecrets(t *testing.T) {
	environment := personalTokenAcceptanceEnvironment(
		[]string{"SESSION_SECRET=required", "APP_ADDRESS=old", "UNCHANGED=value"},
		map[string]string{"APP_ADDRESS": "new"},
	)
	joined := "\x00" + strings.Join(environment, "\x00") + "\x00"
	for _, want := range []string{"\x00SESSION_SECRET=required\x00", "\x00APP_ADDRESS=new\x00", "\x00UNCHANGED=value\x00"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("acceptance environment omitted %q: %q", want, environment)
		}
	}
	if strings.Contains(joined, "\x00APP_ADDRESS=old\x00") {
		t.Fatalf("acceptance environment retained replaced address: %q", environment)
	}
}

func TestGeneratedPersonalTokenPostgresWorkflow(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}

	root := projectRoot(t)
	scratch := t.TempDir()
	forgeBinary := filepath.Join(scratch, "forge")
	serverBinary := filepath.Join(scratch, "personal-token-server")
	if runtime.GOOS == "windows" {
		forgeBinary += ".exe"
		serverBinary += ".exe"
	}
	baseEnvironment := append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := generatedCommand(root, baseEnvironment, "go", "build", "-o", forgeBinary, "./cmd/forge"); err != nil {
		t.Fatalf("build forge CLI: %v\n%s", err, output)
	}
	directory := filepath.Join(scratch, "token-app")
	if output, err := generatedCommand(scratch, baseEnvironment, forgeBinary, "new", "token-app", "--module", "example.com/tokenapp", "--replace", root); err != nil {
		t.Fatalf("forge new: %v\n%s", err, output)
	}
	manifest, err := os.ReadFile(filepath.Join(directory, "forge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte("version: 14")) {
		t.Fatalf("fresh personal-token application is not format 14:\n%s", manifest)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "make:resource", "Category", "--field", "name:string"); err != nil {
		t.Fatalf("forge make:resource Category: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "make:resource", "Issue", "--field", "title:string", "--belongs-to", "category:Category"); err != nil {
		t.Fatalf("forge make:resource Issue: %v\n%s", err, output)
	}
	personalTokenAcceptanceCustomizeIssueAuthorization(t, directory)
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "views:compile", "--check"); err != nil {
		t.Fatalf("fresh personal-token views are stale: %v\n%s", err, output)
	}
	// Compile and exercise generated unit tests before requiring a live schema.
	if output, err := generatedCommand(directory, baseEnvironment, "go", "test", "./..."); err != nil {
		t.Fatalf("fresh personal-token application tests: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, "go", "build", "-o", serverBinary, "./cmd/server"); err != nil {
		t.Fatalf("build generated personal-token server: %v\n%s", err, output)
	}

	schema := fmt.Sprintf("goforge_personal_token_acceptance_%d", time.Now().UnixNano())
	adminDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.ExecContext(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create isolated PostgreSQL schema: %v", err)
	}
	t.Cleanup(func() {
		if _, dropErr := adminDB.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); dropErr != nil {
			t.Errorf("drop isolated PostgreSQL schema: %v", dropErr)
		}
		_ = adminDB.Close()
	})
	isolatedURL, err := postgresSchemaURL(databaseURL, schema)
	if err != nil {
		t.Fatal(err)
	}
	applicationURL := "http://application.example.test"
	applicationEnvironment := personalTokenAcceptanceEnvironment(baseEnvironment, map[string]string{
		"DATABASE_URL":            isolatedURL,
		"APP_ENV":                 "local",
		"APP_URL":                 applicationURL,
		"SESSION_SECRET":          strings.Repeat("s", 32),
		"AUTH_PASSWORD_RESET_TTL": "30m",
		"MAIL_FROM":               "GoForge Acceptance <no-reply@example.test>",
		"MAIL_SMTP_ADDRESS":       "127.0.0.1:1",
		"MAIL_SMTP_TLS":           "none",
		"MAIL_SMTP_SERVER_NAME":   "localhost",
		"MAIL_SMTP_TIMEOUT":       "1s",
		"MAIL_OUTBOX_KEY":         base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x6b}, 32)),
		"TRUSTED_PROXIES":         "127.0.0.1/32",
	})
	if output, err := generatedCommand(directory, applicationEnvironment, forgeBinary, "migrate"); err != nil {
		t.Fatalf("migrate personal-token schema: %v\n%s", err, output)
	} else if !strings.Contains(output, "000006_create_personal_access_tokens") {
		t.Fatalf("personal-token migration was not applied:\n%s", output)
	}

	db, err := sql.Open("pgx", isolatedURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	address := freeAddress(t)
	serverEnvironment := personalTokenAcceptanceEnvironment(applicationEnvironment, map[string]string{"APP_ADDRESS": address})
	server, firstOutput := startGeneratedServer(t, serverBinary, scratch, serverEnvironment)
	serverRunning := true
	t.Cleanup(func() {
		if serverRunning {
			stopCommandProcess(t, server, false)
		}
	})
	baseURL := "http://" + address
	waitForHealth(t, baseURL, firstOutput)

	const password = "a secure passphrase"
	stamp := time.Now().UnixNano()
	owner := clientWithCookies(t)
	other := clientWithCookies(t)
	admin := clientWithCookies(t)
	readonly := clientWithCookies(t)
	personalTokenAcceptanceRegister(t, owner, baseURL, "Owner", fmt.Sprintf("owner-%d@example.test", stamp), password)
	personalTokenAcceptanceRegister(t, other, baseURL, "Other", fmt.Sprintf("other-%d@example.test", stamp), password)
	personalTokenAcceptanceRegister(t, admin, baseURL, "Administrator", "admin@example.com", password)
	personalTokenAcceptanceRegister(t, readonly, baseURL, "Read Only", "readonly@example.com", password)

	var materials []personalTokenAcceptanceMaterial
	primary, primaryMaterial, primaryCreation := personalTokenAcceptanceCreateJSON(t, owner, baseURL, "automation", password, 30)
	materials = append(materials, primaryMaterial)
	personalTokenAcceptanceAssertPrivate(t, primaryCreation)
	if primary.ID < 1 || primary.Name != "automation" || primary.ExpiresAt == "" || primary.CreatedAt == "" {
		t.Fatal("JSON token creation omitted safe metadata")
	}
	listResponse := personalTokenAcceptanceMustRequest(t, owner, http.MethodGet, baseURL+"/auth/tokens", "", nil)
	personalTokenAcceptanceAssertList(t, listResponse, "automation", materials...)

	expiring, expiringMaterial, _ := personalTokenAcceptanceCreateJSON(t, owner, baseURL, "expires", password, 1)
	materials = append(materials, expiringMaterial)
	individual, individualMaterial, _ := personalTokenAcceptanceCreateJSON(t, owner, baseURL, "individual", password, 30)
	materials = append(materials, individualMaterial)

	securityPage := personalTokenAcceptanceMustRequest(t, owner, http.MethodGet, baseURL+"/settings/security", "", nil)
	if securityPage.Status != http.StatusOK {
		t.Fatalf("security page status = %d, want 200", securityPage.Status)
	}
	personalTokenAcceptanceAssertNoOutputSecrets(t, securityPage.Body, materials...)
	csrf := browserCSRF(t, securityPage.Body)
	browserCreation := personalTokenAcceptanceMustForm(t, owner, http.MethodPost, baseURL+"/settings/tokens", url.Values{
		"_token":           {csrf},
		"name":             {"browser token"},
		"current_password": {password},
		"expires_in_days":  {"7"},
	})
	if browserCreation.Status != http.StatusCreated {
		t.Fatalf("browser token creation status = %d, want 201", browserCreation.Status)
	}
	personalTokenAcceptanceAssertPrivate(t, browserCreation)
	browserMaterial := personalTokenAcceptanceMaterialFromHTML(t, browserCreation.Body)
	materials = append(materials, browserMaterial)
	var browserTokenID int64
	if err := db.QueryRow(`SELECT id FROM personal_access_tokens WHERE name = 'browser token'`).Scan(&browserTokenID); err != nil {
		t.Fatalf("read browser token ID: %v", err)
	}
	securityPage = personalTokenAcceptanceMustRequest(t, owner, http.MethodGet, baseURL+"/settings/security", "", nil)
	if securityPage.Status != http.StatusOK || !strings.Contains(securityPage.Body, "browser token") {
		t.Fatalf("browser token list status=%d or omitted safe name", securityPage.Status)
	}
	personalTokenAcceptanceAssertNoOutputSecrets(t, securityPage.Body, materials...)

	personalTokenAcceptanceAssertStoredShape(t, db, primary, primaryMaterial)
	var ownerID, ownerCredentialVersion int64
	if err := db.QueryRow(`SELECT user_id, credential_version FROM personal_access_tokens WHERE id = $1`, primary.ID).Scan(&ownerID, &ownerCredentialVersion); err != nil {
		t.Fatalf("read owner token account: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO personal_access_tokens (user_id, name, selector, secret_digest, credential_version, expires_at)
		VALUES ($1, 'hostile selector', $2, $3, $4, NOW() + INTERVAL '1 day')
	`, ownerID, strings.Repeat("!", 22), bytes.Repeat([]byte{0x42}, sha256.Size), ownerCredentialVersion); err == nil {
		t.Fatal("personal token selector constraint accepted invalid 22-byte alphabet")
	}
	personalTokenAcceptanceAssertNoSessionSecrets(t, db, materials...)

	otherToken, otherMaterial, _ := personalTokenAcceptanceCreateJSON(t, other, baseURL, "other automation", password, 30)
	_ = otherToken
	materials = append(materials, otherMaterial)
	_, adminMaterial, _ := personalTokenAcceptanceCreateJSON(t, admin, baseURL, "admin automation", password, 30)
	materials = append(materials, adminMaterial)
	_, readonlyMaterial, _ := personalTokenAcceptanceCreateJSON(t, readonly, baseURL, "readonly automation", password, 30)
	materials = append(materials, readonlyMaterial)
	crossRevoke := personalTokenAcceptanceMustRequest(t, other, http.MethodDelete, fmt.Sprintf("%s/auth/tokens/%d", baseURL, primary.ID), "", nil)
	if crossRevoke.Status != http.StatusNotFound {
		t.Fatalf("cross-owner token revoke status = %d, want 404", crossRevoke.Status)
	}

	sessionsBefore := personalTokenAcceptanceSessions(t, db)
	unknownIssued, err := securitytoken.Issue()
	if err != nil {
		t.Fatal(err)
	}
	knownPresented := strings.TrimPrefix(primaryMaterial.Raw, personalTokenAcceptancePrefix)
	knownSelector, _, _ := strings.Cut(knownPresented, ".")
	_, unknownSecret, _ := strings.Cut(unknownIssued.Presented, ".")
	wrongSecret := personalTokenAcceptancePrefix + knownSelector + "." + unknownSecret
	unknownToken := personalTokenAcceptancePrefix + unknownIssued.Presented
	materials = append(materials, personalTokenAcceptanceMaterialFromRaw(t, unknownToken))
	invalidHeaders := [][]string{
		{"Bearer malformed"},
		{"Bearer " + wrongSecret},
		{"Bearer " + unknownToken},
		{"Basic " + primaryMaterial.Raw},
		{"Bearer " + primaryMaterial.Raw, "Bearer " + primaryMaterial.Raw},
		{"Bearer " + strings.Repeat("a", 1024)},
		{"Bearer  " + primaryMaterial.Raw},
	}
	var unauthorized personalTokenAcceptanceResponse
	for index, headers := range invalidHeaders {
		response := personalTokenAcceptanceMustRequest(t, owner, http.MethodGet, baseURL+"/auth/me", "", headers)
		if response.Status != http.StatusUnauthorized || response.Header.Get("WWW-Authenticate") != "Bearer" || len(response.Header.Values("Set-Cookie")) != 0 {
			t.Fatalf("invalid bearer case %d: status=%d challenge=%q cookies=%d", index, response.Status, response.Header.Get("WWW-Authenticate"), len(response.Header.Values("Set-Cookie")))
		}
		if index == 0 {
			unauthorized = response
		} else if response.Body != unauthorized.Body {
			t.Fatalf("invalid bearer case %d returned a distinguishable body", index)
		}
		personalTokenAcceptanceAssertNoOutputSecrets(t, response.Body, materials...)
	}
	if sessionsAfter := personalTokenAcceptanceSessions(t, db); !reflect.DeepEqual(sessionsAfter, sessionsBefore) {
		t.Fatal("authoritative invalid Bearer headers loaded, refreshed, or saved a valid cookie session")
	}

	me := personalTokenAcceptanceMustRequest(t, &http.Client{Timeout: 5 * time.Second}, http.MethodGet, baseURL+"/auth/me", "", []string{"Bearer " + primaryMaterial.Raw})
	if me.Status != http.StatusOK || !strings.Contains(me.Body, "Owner") || len(me.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("valid bearer /auth/me status=%d cookies=%d", me.Status, len(me.Header.Values("Set-Cookie")))
	}
	personalTokenAcceptanceAssertNoOutputSecrets(t, me.Body, materials...)

	bare := &http.Client{Timeout: 5 * time.Second}
	ownerCategory := personalTokenAcceptanceCreateResource(t, bare, baseURL+"/categories", `{"name":"Owner category"}`, primaryMaterial)
	ownerAlternateCategory := personalTokenAcceptanceCreateResource(t, bare, baseURL+"/categories", `{"name":"Owner alternate category"}`, primaryMaterial)
	otherCategory := personalTokenAcceptanceCreateResource(t, bare, baseURL+"/categories", `{"name":"Other category"}`, otherMaterial)
	adminCategory := personalTokenAcceptanceCreateResource(t, bare, baseURL+"/categories", `{"name":"Admin category"}`, adminMaterial)
	readonlyCategory := personalTokenAcceptanceCreateResource(t, bare, baseURL+"/categories", `{"name":"Readonly category"}`, readonlyMaterial)
	createdIssue := personalTokenAcceptanceCreateResource(t, bare, baseURL+"/issues",
		fmt.Sprintf(`{"title":"Bearer issue","category_id":%d}`, ownerCategory.ID), primaryMaterial)
	if createdIssue.UserID < 1 || createdIssue.CategoryID != ownerCategory.ID || createdIssue.Version != 1 {
		t.Fatalf("bearer resource create returned invalid owner/relationship metadata: %#v", createdIssue)
	}
	issueURL := fmt.Sprintf("%s/issues/%d", baseURL, createdIssue.ID)

	crossCreate := personalTokenAcceptanceMustRequest(t, bare, http.MethodPost, baseURL+"/issues",
		fmt.Sprintf(`{"title":"cross category","category_id":%d}`, otherCategory.ID), []string{"Bearer " + primaryMaterial.Raw})
	if crossCreate.Status != http.StatusUnprocessableEntity || !strings.Contains(crossCreate.Body, "association.invalid") {
		t.Fatalf("owner cross-category create status=%d, want 422 association.invalid: %s", crossCreate.Status, crossCreate.Body)
	}
	crossUpdate := personalTokenAcceptanceMustRequest(t, bare, http.MethodPut, issueURL,
		fmt.Sprintf(`{"title":"cross category update","category_id":%d,"version":%d}`, otherCategory.ID, createdIssue.Version), []string{"Bearer " + primaryMaterial.Raw})
	if crossUpdate.Status != http.StatusUnprocessableEntity || !strings.Contains(crossUpdate.Body, "association.invalid") {
		t.Fatalf("owner cross-category update status=%d, want 422 association.invalid: %s", crossUpdate.Status, crossUpdate.Body)
	}

	otherIssue := personalTokenAcceptanceCreateResource(t, bare, baseURL+"/issues",
		fmt.Sprintf(`{"title":"Other issue","category_id":%d}`, otherCategory.ID), otherMaterial)
	readonlyIssue := personalTokenAcceptanceCreateResource(t, bare, baseURL+"/issues",
		fmt.Sprintf(`{"title":"Readonly issue","category_id":%d}`, readonlyCategory.ID), readonlyMaterial)
	ownerList := personalTokenAcceptanceMustRequest(t, &http.Client{Timeout: 5 * time.Second}, http.MethodGet, baseURL+"/issues", "", []string{"Bearer " + primaryMaterial.Raw})
	if ownerList.Status != http.StatusOK || !strings.Contains(ownerList.Body, "Bearer issue") || strings.Contains(ownerList.Body, "Other issue") {
		t.Fatalf("bearer resource list status=%d or omitted owner row", ownerList.Status)
	}
	otherShow := personalTokenAcceptanceMustRequest(t, &http.Client{Timeout: 5 * time.Second}, http.MethodGet, issueURL, "", []string{"Bearer " + otherMaterial.Raw})
	if otherShow.Status != http.StatusNotFound {
		t.Fatalf("cross-owner bearer resource show status=%d, want 404", otherShow.Status)
	}
	missingShow := personalTokenAcceptanceMustRequest(t, bare, http.MethodGet, baseURL+"/issues/9223372036854775000", "", []string{"Bearer " + otherMaterial.Raw})
	if missingShow.Status != http.StatusNotFound || missingShow.Body != otherShow.Body {
		t.Fatalf("cross-owner and missing bearer rows were distinguishable: cross=%d %q missing=%d %q", otherShow.Status, otherShow.Body, missingShow.Status, missingShow.Body)
	}
	otherList := personalTokenAcceptanceMustRequest(t, &http.Client{Timeout: 5 * time.Second}, http.MethodGet, baseURL+"/issues", "", []string{"Bearer " + otherMaterial.Raw})
	if otherList.Status != http.StatusOK || strings.Contains(otherList.Body, "Bearer issue") {
		t.Fatalf("cross-owner bearer list status=%d or disclosed owner row", otherList.Status)
	}
	updatedIssue := personalTokenAcceptanceMustRequest(t, &http.Client{Timeout: 5 * time.Second}, http.MethodPut, issueURL,
		fmt.Sprintf(`{"title":"Bearer issue updated","category_id":%d,"version":%d}`, ownerCategory.ID, createdIssue.Version), []string{"Bearer " + primaryMaterial.Raw})
	if updatedIssue.Status != http.StatusOK || !strings.Contains(updatedIssue.Body, "Bearer issue updated") {
		t.Fatalf("bearer resource update status=%d or omitted update", updatedIssue.Status)
	}
	currentIssue := personalTokenAcceptanceDecodeResource(t, updatedIssue.Body)
	stale := personalTokenAcceptanceMustRequest(t, bare, http.MethodPut, issueURL,
		fmt.Sprintf(`{"title":"stale overwrite","category_id":%d,"version":%d}`, ownerCategory.ID, createdIssue.Version), []string{"Bearer " + primaryMaterial.Raw})
	if stale.Status != http.StatusConflict {
		t.Fatalf("stale bearer update status=%d, want 409: %s", stale.Status, stale.Body)
	}
	afterStale := personalTokenAcceptanceMustRequest(t, bare, http.MethodGet, issueURL, "", []string{"Bearer " + primaryMaterial.Raw})
	if afterStale.Status != http.StatusOK || !strings.Contains(afterStale.Body, "Bearer issue updated") {
		t.Fatalf("stale update changed resource: status=%d body=%s", afterStale.Status, afterStale.Body)
	}

	readonlyDelete := personalTokenAcceptanceMustRequest(t, bare, http.MethodDelete, fmt.Sprintf("%s/issues/%d", baseURL, readonlyIssue.ID), "", []string{"Bearer " + readonlyMaterial.Raw})
	if readonlyDelete.Status != http.StatusForbidden {
		t.Fatalf("explicit ActionDelete denial status=%d, want 403: %s", readonlyDelete.Status, readonlyDelete.Body)
	}
	readonlyAfterDenial := personalTokenAcceptanceMustRequest(t, bare, http.MethodGet, fmt.Sprintf("%s/issues/%d", baseURL, readonlyIssue.ID), "", []string{"Bearer " + readonlyMaterial.Raw})
	if readonlyAfterDenial.Status != http.StatusOK || !strings.Contains(readonlyAfterDenial.Body, "Readonly issue") {
		t.Fatalf("denied ActionDelete changed row: status=%d body=%s", readonlyAfterDenial.Status, readonlyAfterDenial.Body)
	}
	adminList := personalTokenAcceptanceMustRequest(t, bare, http.MethodGet, baseURL+"/issues", "", []string{"Bearer " + adminMaterial.Raw})
	if adminList.Status != http.StatusOK || !strings.Contains(adminList.Body, "Bearer issue updated") || !strings.Contains(adminList.Body, "Other issue") || !strings.Contains(adminList.Body, "Readonly issue") {
		t.Fatalf("AccessAll list did not include foreign rows: status=%d body=%s", adminList.Status, adminList.Body)
	}
	adminCrossOwnerRelationship := personalTokenAcceptanceMustRequest(t, bare, http.MethodPut, issueURL,
		fmt.Sprintf(`{"title":"Admin all-scope update","category_id":%d,"version":%d}`, otherCategory.ID, currentIssue.Version), []string{"Bearer " + adminMaterial.Raw})
	if adminCrossOwnerRelationship.Status != http.StatusUnprocessableEntity || !strings.Contains(adminCrossOwnerRelationship.Body, "association.invalid") {
		t.Fatalf("AccessAll update crossed the resource-owner relationship invariant: status=%d body=%s", adminCrossOwnerRelationship.Status, adminCrossOwnerRelationship.Body)
	}
	adminUpdate := personalTokenAcceptanceMustRequest(t, bare, http.MethodPut, issueURL,
		fmt.Sprintf(`{"title":"Admin all-scope update","category_id":%d,"version":%d}`, ownerAlternateCategory.ID, currentIssue.Version), []string{"Bearer " + adminMaterial.Raw})
	adminUpdatedIssue := personalTokenAcceptanceDecodeResource(t, adminUpdate.Body)
	if adminUpdate.Status != http.StatusOK || adminUpdatedIssue.UserID != createdIssue.UserID || adminUpdatedIssue.CategoryID != ownerAlternateCategory.ID || adminUpdatedIssue.Title != "Admin all-scope update" {
		t.Fatalf("AccessAll update did not preserve owner and same-owner relationship: status=%d resource=%#v body=%s", adminUpdate.Status, adminUpdatedIssue, adminUpdate.Body)
	}
	adminCrossOwnerCreate := personalTokenAcceptanceMustRequest(t, bare, http.MethodPost, baseURL+"/issues",
		fmt.Sprintf(`{"title":"Admin invalid category","category_id":%d}`, ownerCategory.ID), []string{"Bearer " + adminMaterial.Raw})
	if adminCrossOwnerCreate.Status != http.StatusUnprocessableEntity || !strings.Contains(adminCrossOwnerCreate.Body, "association.invalid") {
		t.Fatalf("AccessAll create crossed the actor-owner relationship invariant: status=%d body=%s", adminCrossOwnerCreate.Status, adminCrossOwnerCreate.Body)
	}
	adminCreated := personalTokenAcceptanceCreateResource(t, bare, baseURL+"/issues",
		fmt.Sprintf(`{"title":"Admin own-category create","category_id":%d}`, adminCategory.ID), adminMaterial)
	if adminCreated.CategoryID != adminCategory.ID || adminCreated.UserID == createdIssue.UserID {
		t.Fatalf("AccessAll create did not retain actor ownership and same-owner relationship: %#v", adminCreated)
	}
	adminDelete := personalTokenAcceptanceMustRequest(t, bare, http.MethodDelete, fmt.Sprintf("%s/issues/%d", baseURL, otherIssue.ID), "", []string{"Bearer " + adminMaterial.Raw})
	if adminDelete.Status != http.StatusNoContent {
		t.Fatalf("AccessAll delete of foreign row status=%d, want 204: %s", adminDelete.Status, adminDelete.Body)
	}

	for index, boundary := range []struct {
		method string
		target string
		body   string
		want   int
	}{
		{http.MethodGet, baseURL + "/auth/tokens", "", http.StatusUnauthorized},
		{http.MethodPost, baseURL + "/auth/tokens", `{"name":"forbidden","current_password":"a secure passphrase","expires_in_days":1}`, http.StatusUnauthorized},
		{http.MethodDelete, fmt.Sprintf("%s/auth/tokens/%d", baseURL, primary.ID), "", http.StatusUnauthorized},
		{http.MethodPut, baseURL + "/auth/password", `{"current_password":"a secure passphrase","password":"different secure passphrase","password_confirmation":"different secure passphrase"}`, http.StatusUnauthorized},
	} {
		response := personalTokenAcceptanceMustRequest(t, &http.Client{Timeout: 5 * time.Second}, boundary.method, boundary.target, boundary.body, []string{"Bearer " + primaryMaterial.Raw})
		if response.Status != boundary.want {
			t.Fatalf("session-only JSON boundary %d status=%d, want %d", index, response.Status, boundary.want)
		}
	}
	noRedirect := clientWithCookiesNoRedirect(t)
	for index, target := range []string{baseURL + "/settings/security", baseURL + "/app/issues"} {
		response := personalTokenAcceptanceMustRequest(t, noRedirect, http.MethodGet, target, "", []string{"Bearer " + primaryMaterial.Raw})
		if response.Status != http.StatusSeeOther || response.Header.Get("Location") != "/login" {
			t.Fatalf("session-only browser boundary %d status=%d location=%q", index, response.Status, response.Header.Get("Location"))
		}
	}

	if _, err := db.Exec(`UPDATE personal_access_tokens SET created_at = NOW() - INTERVAL '2 days', expires_at = NOW() - INTERVAL '1 day' WHERE id = $1`, expiring.ID); err != nil {
		t.Fatalf("expire personal token: %v", err)
	}
	expired := personalTokenAcceptanceMustRequest(t, owner, http.MethodGet, baseURL+"/auth/me", "", []string{"Bearer " + expiringMaterial.Raw})
	if expired.Status != http.StatusUnauthorized || len(expired.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("expired bearer status=%d cookies=%d", expired.Status, len(expired.Header.Values("Set-Cookie")))
	}

	csrf = browserCSRF(t, securityPage.Body)
	ownerNoRedirect := &http.Client{
		Jar:           owner.Jar,
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	browserRevoked := personalTokenAcceptanceMustForm(t, ownerNoRedirect, http.MethodPost, fmt.Sprintf("%s/settings/tokens/%d/revoke", baseURL, browserTokenID), url.Values{"_token": {csrf}})
	if browserRevoked.Status != http.StatusSeeOther || browserRevoked.Header.Get("Location") != "/settings/security" {
		t.Fatalf("browser token revoke status=%d location=%q", browserRevoked.Status, browserRevoked.Header.Get("Location"))
	}
	if response := personalTokenAcceptanceMustRequest(t, owner, http.MethodGet, baseURL+"/auth/me", "", []string{"Bearer " + browserMaterial.Raw}); response.Status != http.StatusUnauthorized {
		t.Fatalf("browser-revoked bearer status=%d, want 401", response.Status)
	}
	jsonRevoked := personalTokenAcceptanceMustRequest(t, owner, http.MethodDelete, fmt.Sprintf("%s/auth/tokens/%d", baseURL, individual.ID), "", nil)
	if jsonRevoked.Status != http.StatusNoContent {
		t.Fatalf("JSON token revoke status=%d, want 204", jsonRevoked.Status)
	}
	if response := personalTokenAcceptanceMustRequest(t, owner, http.MethodGet, baseURL+"/auth/me", "", []string{"Bearer " + individualMaterial.Raw}); response.Status != http.StatusUnauthorized {
		t.Fatalf("JSON-revoked bearer status=%d, want 401", response.Status)
	}

	stopCommandProcess(t, server, false)
	serverRunning = false
	restartedAddress := freeAddress(t)
	restartedEnvironment := personalTokenAcceptanceEnvironment(applicationEnvironment, map[string]string{"APP_ADDRESS": restartedAddress})
	restarted, restartedOutput := startGeneratedServer(t, serverBinary, scratch, restartedEnvironment)
	restartedRunning := true
	t.Cleanup(func() {
		if restartedRunning {
			stopCommandProcess(t, restarted, false)
		}
	})
	baseURL = "http://" + restartedAddress
	waitForHealth(t, baseURL, restartedOutput)
	if response := personalTokenAcceptanceMustRequest(t, owner, http.MethodGet, baseURL+"/auth/me", "", []string{"Bearer " + primaryMaterial.Raw}); response.Status != http.StatusOK {
		t.Fatalf("bearer did not survive process restart: status=%d", response.Status)
	}
	for index, revokedMaterial := range []personalTokenAcceptanceMaterial{browserMaterial, individualMaterial} {
		if response := personalTokenAcceptanceMustRequest(t, owner, http.MethodGet, baseURL+"/auth/me", "", []string{"Bearer " + revokedMaterial.Raw}); response.Status != http.StatusUnauthorized {
			t.Fatalf("revoked bearer %d survived process restart: status=%d", index, response.Status)
		}
	}

	changedPassword := "a replacement passphrase"
	passwordChange := personalTokenAcceptanceMustRequest(t, owner, http.MethodPut, baseURL+"/auth/password",
		fmt.Sprintf(`{"current_password":%q,"password":%q,"password_confirmation":%q}`, password, changedPassword, changedPassword), nil)
	if passwordChange.Status != http.StatusNoContent {
		t.Fatalf("password change status=%d, want 204", passwordChange.Status)
	}
	if response := personalTokenAcceptanceMustRequest(t, owner, http.MethodGet, baseURL+"/auth/me", "", []string{"Bearer " + primaryMaterial.Raw}); response.Status != http.StatusUnauthorized {
		t.Fatalf("prior-generation bearer status=%d after password change, want 401", response.Status)
	}
	if response := personalTokenAcceptanceMustRequest(t, owner, http.MethodPost, baseURL+"/issues", `{"name":"invalidated generation"}`, []string{"Bearer " + primaryMaterial.Raw}); response.Status != http.StatusUnauthorized {
		t.Fatalf("prior-generation bearer resource status=%d after password change, want 401", response.Status)
	}

	logoutAllClient := clientWithCookies(t)
	logoutAllEmail := fmt.Sprintf("logout-all-%d@example.test", stamp)
	personalTokenAcceptanceRegister(t, logoutAllClient, baseURL, "Logout All", logoutAllEmail, password)
	_, logoutAllMaterial, _ := personalTokenAcceptanceCreateJSON(t, logoutAllClient, baseURL, "logout-all generation", password, 30)
	materials = append(materials, logoutAllMaterial)
	logoutAll := personalTokenAcceptanceMustRequest(t, logoutAllClient, http.MethodPost, baseURL+"/auth/logout-all", `{}`, nil)
	if logoutAll.Status != http.StatusNoContent {
		t.Fatalf("logout-all status=%d, want 204: %s", logoutAll.Status, logoutAll.Body)
	}
	if response := personalTokenAcceptanceMustRequest(t, bare, http.MethodGet, baseURL+"/auth/me", "", []string{"Bearer " + logoutAllMaterial.Raw}); response.Status != http.StatusUnauthorized {
		t.Fatalf("prior-generation bearer status=%d after logout-all, want 401", response.Status)
	}

	resetClient := clientWithCookies(t)
	resetEmail := fmt.Sprintf("token-reset-%d@example.test", stamp)
	personalTokenAcceptanceRegister(t, resetClient, baseURL, "Token Reset", resetEmail, password)
	_, resetMaterial, _ := personalTokenAcceptanceCreateJSON(t, resetClient, baseURL, "reset generation", password, 30)
	materials = append(materials, resetMaterial)
	resetIssued, err := securitytoken.Issue()
	if err != nil {
		t.Fatal(err)
	}
	var resetUserID, resetCredentialVersion int64
	if err := db.QueryRow(`SELECT id, credential_version FROM users WHERE email = $1`, resetEmail).Scan(&resetUserID, &resetCredentialVersion); err != nil {
		t.Fatalf("read password-reset token account: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO password_reset_tokens (selector, user_id, secret_digest, credential_version, expires_at)
		VALUES ($1, $2, $3, $4, NOW() + INTERVAL '30 minutes')
	`, resetIssued.Selector, resetUserID, resetIssued.Digest(), resetCredentialVersion); err != nil {
		t.Fatalf("seed password-reset credential: %v", err)
	}
	resetPassword := "a reset replacement passphrase"
	resetResponse := personalTokenAcceptanceMustRequest(t, bare, http.MethodPost, baseURL+"/auth/password/reset",
		fmt.Sprintf(`{"token":%q,"password":%q,"password_confirmation":%q}`, resetIssued.Presented, resetPassword, resetPassword), nil)
	if resetResponse.Status != http.StatusNoContent || strings.Contains(resetResponse.Body, resetIssued.Presented) {
		t.Fatalf("password reset status=%d or disclosed credential: %s", resetResponse.Status, resetResponse.Body)
	}
	if response := personalTokenAcceptanceMustRequest(t, bare, http.MethodGet, baseURL+"/auth/me", "", []string{"Bearer " + resetMaterial.Raw}); response.Status != http.StatusUnauthorized {
		t.Fatalf("prior-generation bearer status=%d after password reset, want 401", response.Status)
	}

	capClient := clientWithCookies(t)
	personalTokenAcceptanceRegister(t, capClient, baseURL, "Cap", fmt.Sprintf("cap-%d@example.test", stamp), password)
	capClient.Timeout = 30 * time.Second
	type capResult struct {
		response personalTokenAcceptanceResponse
		err      error
	}
	start := make(chan struct{})
	results := make(chan capResult, 12)
	for index := range 12 {
		go func(index int) {
			<-start
			body := fmt.Sprintf(`{"name":"concurrent-%02d","current_password":%q,"expires_in_days":30}`, index, password)
			response, requestErr := personalTokenAcceptanceRequest(capClient, http.MethodPost, baseURL+"/auth/tokens", body, nil)
			results <- capResult{response: response, err: requestErr}
		}(index)
	}
	close(start)
	succeeded, limited := 0, 0
	for range 12 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent token creation: %v", result.err)
		}
		switch result.response.Status {
		case http.StatusCreated:
			var envelope struct {
				Data personalTokenAcceptanceRecord `json:"data"`
			}
			if err := json.Unmarshal([]byte(result.response.Body), &envelope); err != nil {
				t.Fatal("concurrent token creation returned invalid JSON")
			}
			materials = append(materials, personalTokenAcceptanceMaterialFromRaw(t, envelope.Data.Token))
			succeeded++
		case http.StatusUnprocessableEntity:
			if !strings.Contains(result.response.Body, "token.limit") {
				t.Fatalf("concurrent cap 422 omitted token.limit: %s", result.response.Body)
			}
			limited++
		case http.StatusTooManyRequests:
			t.Fatalf("concurrent token cap returned 429 instead of token.limit 422: %s", result.response.Body)
		default:
			t.Fatalf("concurrent token creation status=%d, want 201 or 422", result.response.Status)
		}
	}
	if succeeded != 10 || limited != 2 {
		t.Fatalf("concurrent token cap results: created=%d limited=%d, want 10 and 2", succeeded, limited)
	}
	var capRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM personal_access_tokens WHERE user_id = (SELECT id FROM users WHERE name = 'Cap') AND expires_at > NOW()`).Scan(&capRows); err != nil {
		t.Fatal(err)
	}
	if capRows != 10 {
		t.Fatalf("concurrent cap persisted %d live rows, want 10", capRows)
	}

	personalTokenAcceptanceAssertNoSessionSecrets(t, db, materials...)
	personalTokenAcceptanceAssertNoDatabaseSecrets(t, db, materials...)
	personalTokenAcceptanceAssertNoOutputSecrets(t, firstOutput.String(), materials...)
	personalTokenAcceptanceAssertNoOutputSecrets(t, restartedOutput.String(), materials...)
	if strings.Contains(firstOutput.String(), resetIssued.Presented) || strings.Contains(restartedOutput.String(), resetIssued.Presented) {
		t.Fatal("server logs retained the password-reset credential used for generation invalidation")
	}
	if strings.Contains(firstOutput.String(), strings.Repeat("a", 64)) || strings.Contains(restartedOutput.String(), strings.Repeat("a", 64)) {
		t.Fatal("server logs retained the oversized Authorization credential")
	}
	stopCommandProcess(t, restarted, true)
	restartedRunning = false
}

func personalTokenAcceptanceRegister(t *testing.T, client *http.Client, baseURL, name, email, password string) {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"email":%q,"password":%q,"password_confirmation":%q}`, name, email, password, password)
	response := personalTokenAcceptanceMustRequest(t, client, http.MethodPost, baseURL+"/auth/register", body, nil)
	if response.Status != http.StatusCreated {
		t.Fatalf("register %s: status=%d, want 201", name, response.Status)
	}
}

func personalTokenAcceptanceEnvironment(base []string, overrides map[string]string) []string {
	blocked := make(map[string]struct{}, len(overrides))
	for key := range overrides {
		blocked[strings.ToUpper(key)] = struct{}{}
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, replace := blocked[strings.ToUpper(key)]; !replace {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func personalTokenAcceptanceCustomizeIssueAuthorization(t *testing.T, directory string) {
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

func personalTokenAcceptanceCreateResource(t *testing.T, client *http.Client, target, body string, material personalTokenAcceptanceMaterial) personalTokenAcceptanceResource {
	t.Helper()
	response := personalTokenAcceptanceMustRequest(t, client, http.MethodPost, target, body, []string{"Bearer " + material.Raw})
	if response.Status != http.StatusCreated {
		t.Fatalf("create resource %s: status=%d, want 201: %s", target, response.Status, response.Body)
	}
	resource := personalTokenAcceptanceDecodeResource(t, response.Body)
	if resource.ID < 1 || resource.UserID < 1 || resource.Version != 1 {
		t.Fatalf("created resource has invalid ownership/version metadata: %#v", resource)
	}
	return resource
}

func personalTokenAcceptanceDecodeResource(t *testing.T, body string) personalTokenAcceptanceResource {
	t.Helper()
	var envelope struct {
		Data personalTokenAcceptanceResource `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode generated resource: %v: %s", err, body)
	}
	return envelope.Data
}

func personalTokenAcceptanceCreateJSON(t *testing.T, client *http.Client, baseURL, name, password string, days int) (personalTokenAcceptanceRecord, personalTokenAcceptanceMaterial, personalTokenAcceptanceResponse) {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"current_password":%q,"expires_in_days":%d}`, name, password, days)
	response := personalTokenAcceptanceMustRequest(t, client, http.MethodPost, baseURL+"/auth/tokens", body, nil)
	if response.Status != http.StatusCreated {
		t.Fatalf("create personal token %q: status=%d, want 201", name, response.Status)
	}
	var envelope struct {
		Data personalTokenAcceptanceRecord `json:"data"`
	}
	if err := json.Unmarshal([]byte(response.Body), &envelope); err != nil {
		t.Fatalf("decode personal token creation %q: %v", name, err)
	}
	material := personalTokenAcceptanceMaterialFromRaw(t, envelope.Data.Token)
	personalTokenAcceptanceAssertPrivate(t, response)
	return envelope.Data, material, response
}

func personalTokenAcceptanceMaterialFromHTML(t *testing.T, body string) personalTokenAcceptanceMaterial {
	t.Helper()
	raw := personalTokenAcceptancePattern.FindString(body)
	if raw == "" {
		t.Fatal("browser token creation response omitted the one-time token")
	}
	return personalTokenAcceptanceMaterialFromRaw(t, raw)
}

func personalTokenAcceptanceMaterialFromRaw(t *testing.T, raw string) personalTokenAcceptanceMaterial {
	t.Helper()
	if !strings.HasPrefix(raw, personalTokenAcceptancePrefix) || !personalTokenAcceptancePattern.MatchString(raw) || len(raw) != len(personalTokenAcceptancePrefix)+66 {
		t.Fatal("personal token has a non-canonical wire shape")
	}
	presented := strings.TrimPrefix(raw, personalTokenAcceptancePrefix)
	if _, err := securitytoken.Parse(presented); err != nil {
		t.Fatal("personal token did not reuse the canonical security/token shape")
	}
	selector, secret, found := strings.Cut(presented, ".")
	if !found {
		t.Fatal("personal token omitted selector separator")
	}
	return personalTokenAcceptanceMaterial{Raw: raw, Selector: selector, Secret: secret}
}

func personalTokenAcceptanceAssertPrivate(t *testing.T, response personalTokenAcceptanceResponse) {
	t.Helper()
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Referrer-Policy") != "no-referrer" || response.Header.Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("one-time token response omitted privacy headers: cache=%q referrer=%q robots=%q", response.Header.Get("Cache-Control"), response.Header.Get("Referrer-Policy"), response.Header.Get("X-Robots-Tag"))
	}
}

func personalTokenAcceptanceAssertList(t *testing.T, response personalTokenAcceptanceResponse, wantName string, materials ...personalTokenAcceptanceMaterial) {
	t.Helper()
	if response.Status != http.StatusOK {
		t.Fatalf("personal token list status=%d, want 200", response.Status)
	}
	var envelope struct {
		Data []personalTokenAcceptanceRecord `json:"data"`
	}
	if err := json.Unmarshal([]byte(response.Body), &envelope); err != nil {
		t.Fatal("personal token list returned invalid JSON")
	}
	if strings.Contains(response.Body, `"token"`) || strings.Contains(response.Body, "selector") || strings.Contains(response.Body, "digest") {
		t.Fatal("personal token list exposed credential fields")
	}
	found := false
	for _, item := range envelope.Data {
		if item.Name == wantName {
			found = true
		}
		if item.Token != "" {
			t.Fatal("personal token list exposed a token field")
		}
	}
	if !found {
		t.Fatalf("personal token list omitted %q", wantName)
	}
	personalTokenAcceptanceAssertNoOutputSecrets(t, response.Body, materials...)
}

func personalTokenAcceptanceAssertStoredShape(t *testing.T, db *sql.DB, record personalTokenAcceptanceRecord, material personalTokenAcceptanceMaterial) {
	t.Helper()
	rows, err := db.Query(`SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'personal_access_tokens' ORDER BY ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	wantColumns := []string{"id", "user_id", "name", "selector", "secret_digest", "credential_version", "expires_at", "created_at"}
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("personal token columns=%v, want digest-only shape %v", columns, wantColumns)
	}
	var selector string
	var digest []byte
	var generation int64
	if err := db.QueryRow(`SELECT selector, secret_digest, credential_version FROM personal_access_tokens WHERE id = $1`, record.ID).Scan(&selector, &digest, &generation); err != nil {
		t.Fatal(err)
	}
	decodedSecret, err := base64.RawURLEncoding.Strict().DecodeString(material.Secret)
	if err != nil {
		t.Fatal("decode issued secret")
	}
	wantDigest := sha256.Sum256(decodedSecret)
	if selector != material.Selector || len(selector) != 22 || len(digest) != sha256.Size || !bytes.Equal(digest, wantDigest[:]) || generation < 1 {
		t.Fatalf("stored token has invalid selector/digest/generation shape: selector_length=%d digest_length=%d generation=%d", len(selector), len(digest), generation)
	}
}

func personalTokenAcceptanceSessions(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT id, encode(payload, 'base64'), expires_at::text FROM sessions ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var id, payload, expires string
		if err := rows.Scan(&id, &payload, &expires); err != nil {
			t.Fatal(err)
		}
		result[id] = payload + "|" + expires
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func personalTokenAcceptanceAssertNoSessionSecrets(t *testing.T, db *sql.DB, materials ...personalTokenAcceptanceMaterial) {
	t.Helper()
	rows, err := db.Query(`SELECT payload FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		personalTokenAcceptanceAssertNoOutputSecrets(t, string(payload), materials...)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func personalTokenAcceptanceAssertNoDatabaseSecrets(t *testing.T, db *sql.DB, materials ...personalTokenAcceptanceMaterial) {
	t.Helper()
	rows, err := db.Query(`SELECT name, selector, secret_digest FROM personal_access_tokens`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, selector string
		var digest []byte
		if err := rows.Scan(&name, &selector, &digest); err != nil {
			t.Fatal(err)
		}
		for _, material := range materials {
			if strings.Contains(name, material.Raw) || strings.Contains(name, material.Secret) || bytes.Contains(digest, []byte(material.Secret)) || bytes.Contains(digest, []byte(material.Raw)) {
				t.Fatal("personal token storage retained raw secret material")
			}
			// The public selector is expected in this table, but no other row may
			// accidentally contain the complete presented value.
			if selector != material.Selector && strings.Contains(selector, material.Raw) {
				t.Fatal("personal token selector column retained a presented token")
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func personalTokenAcceptanceAssertNoOutputSecrets(t *testing.T, output string, materials ...personalTokenAcceptanceMaterial) {
	t.Helper()
	for _, material := range materials {
		fragments := []string{material.Raw, material.Selector, material.Secret}
		if len(material.Secret) >= 24 {
			fragments = append(fragments, material.Secret[:12], material.Secret[len(material.Secret)-12:])
		}
		for _, fragment := range fragments {
			if fragment != "" && strings.Contains(output, fragment) {
				t.Fatal("response, log, or session payload disclosed personal token material")
			}
		}
	}
}

func personalTokenAcceptanceMustForm(t *testing.T, client *http.Client, method, target string, values url.Values) personalTokenAcceptanceResponse {
	t.Helper()
	request, err := http.NewRequest(method, target, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := personalTokenAcceptanceDo(client, request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func personalTokenAcceptanceMustRequest(t *testing.T, client *http.Client, method, target, body string, authorization []string) personalTokenAcceptanceResponse {
	t.Helper()
	response, err := personalTokenAcceptanceRequest(client, method, target, body, authorization)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func personalTokenAcceptanceRequest(client *http.Client, method, target, body string, authorization []string) (personalTokenAcceptanceResponse, error) {
	request, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		return personalTokenAcceptanceResponse{}, err
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, value := range authorization {
		request.Header.Add("Authorization", value)
	}
	return personalTokenAcceptanceDo(client, request)
}

func personalTokenAcceptanceDo(client *http.Client, request *http.Request) (personalTokenAcceptanceResponse, error) {
	response, err := client.Do(request)
	if err != nil {
		return personalTokenAcceptanceResponse{}, err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		return personalTokenAcceptanceResponse{}, err
	}
	return personalTokenAcceptanceResponse{Status: response.StatusCode, Header: response.Header.Clone(), Body: string(contents)}, nil
}
