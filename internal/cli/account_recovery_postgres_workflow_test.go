package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestGeneratedAccountRecoveryPostgresWorkflow(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}
	root := projectRoot(t)
	scratch := t.TempDir()
	forgeBinary := filepath.Join(scratch, "forge")
	serverBinary := filepath.Join(scratch, "recovery-server")
	workerBinary := filepath.Join(scratch, "recovery-worker")
	if runtime.GOOS == "windows" {
		forgeBinary += ".exe"
		serverBinary += ".exe"
		workerBinary += ".exe"
	}
	baseEnvironment := append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := generatedCommand(root, baseEnvironment, "go", "build", "-o", forgeBinary, "./cmd/forge"); err != nil {
		t.Fatalf("build forge CLI: %v\n%s", err, output)
	}
	directory := filepath.Join(scratch, "recovery-app")
	if output, err := generatedCommand(scratch, baseEnvironment, forgeBinary, "new", "recovery-app", "--module", "example.com/recoveryapp", "--replace", root); err != nil {
		t.Fatalf("forge new: %v\n%s", err, output)
	}
	manifest, err := os.ReadFile(filepath.Join(directory, "forge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte("version: 12")) {
		t.Fatalf("fresh recovery application is not format 12:\n%s", manifest)
	}

	schema := fmt.Sprintf("goforge_recovery_acceptance_%d", time.Now().UnixNano())
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
	outboxKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x6b}, 32))
	const applicationURL = "http://application.example.test:4173"
	applicationEnvironment := jobAcceptanceEnvironment(baseEnvironment, map[string]string{
		"DATABASE_URL":            isolatedURL,
		"APP_ENV":                 "local",
		"APP_URL":                 applicationURL,
		"SESSION_SECRET":          strings.Repeat("s", 32),
		"AUTH_PASSWORD_RESET_TTL": "30m",
		"MAIL_FROM":               "GoForge Acceptance <no-reply@example.test>",
		"MAIL_OUTBOX_KEY":         outboxKey,
		"TRUSTED_PROXIES":         "127.0.0.1/32",
	})
	if output, err := generatedCommand(directory, applicationEnvironment, forgeBinary, "migrate"); err != nil {
		t.Fatalf("migrate account recovery schema: %v\n%s", err, output)
	} else if !strings.Contains(output, "000004_create_account_recovery") {
		t.Fatalf("account recovery migration was not applied:\n%s", output)
	}
	for _, gate := range [][]string{{"test", "-race", "./..."}, {"vet", "./..."}} {
		if output, err := generatedCommand(directory, applicationEnvironment, "go", gate...); err != nil {
			t.Fatalf("fresh account recovery application go %s: %v\n%s", strings.Join(gate, " "), err, output)
		}
	}
	if output, err := generatedCommand(directory, applicationEnvironment, "go", "build", "-o", serverBinary, "./cmd/server"); err != nil {
		t.Fatalf("build generated recovery server: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, applicationEnvironment, "go", "build", "-o", workerBinary, "./cmd/worker"); err != nil {
		t.Fatalf("build generated recovery worker: %v\n%s", err, output)
	}

	smtpServer := startRecoverySMTPServer(t, true)
	address := freeAddress(t)
	serverEnvironment := jobAcceptanceEnvironment(applicationEnvironment, map[string]string{
		"APP_ADDRESS": address, "SESSION_SECRET": strings.Repeat("s", 32),
	})
	server, serverOutput := startGeneratedServer(t, serverBinary, scratch, serverEnvironment)
	serverRunning := true
	t.Cleanup(func() {
		if serverRunning {
			stopCommandProcess(t, server, false)
		}
	})
	baseURL := "http://" + address
	waitForHealth(t, baseURL, serverOutput)

	db, err := sql.Open("pgx", isolatedURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stamp := time.Now().UnixNano()
	knownEmail := fmt.Sprintf("known-%d@example.test", stamp)
	expiredEmail := fmt.Sprintf("expired-%d@example.test", stamp)
	concurrentEmail := fmt.Sprintf("concurrent-%d@example.test", stamp)
	browserEmail := fmt.Sprintf("browser-%d@example.test", stamp)
	rateLimitEmail := fmt.Sprintf("rate-limit-%d@example.test", stamp)
	resetFailureEmail := fmt.Sprintf("reset-failure-%d@example.test", stamp)
	slowFailureEmail := fmt.Sprintf("slow-failure-%d@example.test", stamp)
	failureEmails := []string{
		fmt.Sprintf("token-failure-%d@example.test", stamp),
		fmt.Sprintf("outbox-failure-%d@example.test", stamp),
		fmt.Sprintf("job-failure-%d@example.test", stamp),
	}
	const oldPassword = "a sufficiently secure original passphrase"
	knownSession := clientWithCookies(t)
	expiredSession := clientWithCookies(t)
	concurrentSession := clientWithCookies(t)
	browserSession := clientWithCookies(t)
	for index, registration := range []struct {
		client *http.Client
		name   string
		email  string
	}{
		{knownSession, "Known User", knownEmail},
		{expiredSession, "Expired User", expiredEmail},
		{concurrentSession, "Concurrent User", concurrentEmail},
		{browserSession, "Browser User", browserEmail},
		{clientWithCookies(t), "Rate Limit User", rateLimitEmail},
		{clientWithCookies(t), "Reset Failure User", resetFailureEmail},
		{clientWithCookies(t), "Slow Failure User", slowFailureEmail},
		{clientWithCookies(t), "Token Failure User", failureEmails[0]},
		{clientWithCookies(t), "Outbox Failure User", failureEmails[1]},
		{clientWithCookies(t), "Job Failure User", failureEmails[2]},
	} {
		body := fmt.Sprintf(`{"name":%q,"email":%q,"password":%q,"password_confirmation":%q}`, registration.name, registration.email, oldPassword, oldPassword)
		response := recoveryAcceptanceMustJSON(t, registration.client, http.MethodPost, baseURL+"/auth/register", body, fmt.Sprintf("203.0.113.%d", index+1))
		if response.Status != http.StatusCreated {
			t.Fatalf("register recovery user %d: %d: %s", index+1, response.Status, response.Body)
		}
	}
	if response := recoveryAcceptanceMustJSON(t, knownSession, http.MethodGet, baseURL+"/auth/me", "", "203.0.113.10"); response.Status != http.StatusOK {
		t.Fatalf("pre-reset session is not authenticated: %d: %s", response.Status, response.Body)
	}
	knownVersionBefore := recoveryAcceptanceCredentialVersion(t, db, knownEmail)

	unknownEmail := fmt.Sprintf("absent-%d@example.test", stamp)
	knownBrowser := clientWithCookiesNoRedirect(t)
	unknownBrowser := clientWithCookiesNoRedirect(t)
	knownForm := recoveryAcceptanceMustBrowser(t, knownBrowser, http.MethodGet, baseURL+"/forgot-password", nil, "198.51.100.10")
	unknownForm := recoveryAcceptanceMustBrowser(t, unknownBrowser, http.MethodGet, baseURL+"/forgot-password", nil, "198.51.100.11")
	if knownForm.Status != http.StatusOK || unknownForm.Status != http.StatusOK {
		t.Fatalf("forgot-password forms: known=%d unknown=%d", knownForm.Status, unknownForm.Status)
	}
	knownBrowserResponse := recoveryAcceptanceMustBrowser(t, knownBrowser, http.MethodPost, baseURL+"/forgot-password", url.Values{
		"_token": {browserCSRF(t, knownForm.Body)}, "email": {knownEmail},
	}, "198.51.100.12")
	unknownBrowserResponse := recoveryAcceptanceMustBrowser(t, unknownBrowser, http.MethodPost, baseURL+"/forgot-password", url.Values{
		"_token": {browserCSRF(t, unknownForm.Body)}, "email": {unknownEmail},
	}, "198.51.100.13")
	recoveryAcceptanceAssertEquivalent(t, knownBrowserResponse, unknownBrowserResponse,
		"Location", "Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")
	if knownBrowserResponse.Status != http.StatusSeeOther || knownBrowserResponse.Header.Get("Location") != "/forgot-password" {
		t.Fatalf("browser forgot response = %d %q: %s", knownBrowserResponse.Status, knownBrowserResponse.Header.Get("Location"), knownBrowserResponse.Body)
	}
	recoveryAcceptanceAssertPrivateHeaders(t, knownBrowserResponse)

	forgotBody := func(email string) string { return fmt.Sprintf(`{"email":%q}`, email) }
	knownJSONResponse := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 3 * time.Second}, http.MethodPost, baseURL+"/auth/password/forgot", forgotBody(knownEmail), "198.51.100.14")
	absentBaselineTokens := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens`)
	absentBaselineOutbox := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM mail_outbox`)
	absentBaselineMailJobs := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.deliver.v1'`)
	absentBaselineCleanupJobs := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.cleanup.v1'`)
	unknownJSONResponse := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 3 * time.Second}, http.MethodPost, baseURL+"/auth/password/forgot", forgotBody(unknownEmail), "198.51.100.15")
	recoveryAcceptanceAssertEquivalent(t, knownJSONResponse, unknownJSONResponse,
		"Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")
	if tokens := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens`); tokens != absentBaselineTokens {
		t.Fatalf("absent recovery request changed reset token count from %d to %d", absentBaselineTokens, tokens)
	}
	if outbox := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM mail_outbox`); outbox != absentBaselineOutbox {
		t.Fatalf("absent recovery request changed outbox count from %d to %d", absentBaselineOutbox, outbox)
	}
	if jobs := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.deliver.v1'`); jobs != absentBaselineMailJobs {
		t.Fatalf("absent recovery request changed mail job count from %d to %d", absentBaselineMailJobs, jobs)
	}
	if jobs := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.cleanup.v1'`); jobs != absentBaselineCleanupJobs {
		t.Fatalf("absent recovery request changed cleanup job count from %d to %d", absentBaselineCleanupJobs, jobs)
	}
	if knownJSONResponse.Status != http.StatusAccepted || !strings.Contains(knownJSONResponse.Body, "If an account matches") {
		t.Fatalf("JSON forgot response = %d: %s", knownJSONResponse.Status, knownJSONResponse.Body)
	}
	recoveryAcceptanceAssertPrivateHeaders(t, knownJSONResponse)

	baselineTokens := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens`)
	baselineOutbox := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM mail_outbox`)
	baselineMailJobs := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.deliver.v1'`)
	baselineCleanupJobs := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.cleanup.v1'`)
	for index, stage := range []string{"token", "outbox", "job"} {
		restore := recoveryAcceptanceRejectInsert(t, db, stage)
		response := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 3 * time.Second}, http.MethodPost,
			baseURL+"/auth/password/forgot", forgotBody(failureEmails[index]), fmt.Sprintf("198.51.100.%d", 16+index))
		restore()
		recoveryAcceptanceAssertEquivalent(t, knownJSONResponse, response,
			"Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")
		if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = (SELECT id FROM users WHERE email = $1)`, failureEmails[index]); count != 0 {
			t.Fatalf("%s failure retained %d reset tokens", stage, count)
		}
		if tokens := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens`); tokens != baselineTokens {
			t.Fatalf("%s failure changed reset token count from %d to %d", stage, baselineTokens, tokens)
		}
		if outbox := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM mail_outbox`); outbox != baselineOutbox {
			t.Fatalf("%s failure changed outbox count from %d to %d", stage, baselineOutbox, outbox)
		}
		if jobs := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.deliver.v1'`); jobs != baselineMailJobs {
			t.Fatalf("%s failure changed mail job count from %d to %d", stage, baselineMailJobs, jobs)
		}
		if jobs := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.cleanup.v1'`); jobs != baselineCleanupJobs {
			t.Fatalf("%s failure changed cleanup job count from %d to %d", stage, baselineCleanupJobs, jobs)
		}
	}
	restoreSlowFailure := recoveryAcceptanceDelayTokenInsert(t, db)
	slowFailure := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 3 * time.Second}, http.MethodPost,
		baseURL+"/auth/password/forgot", forgotBody(slowFailureEmail), "198.51.100.19")
	restoreSlowFailure()
	recoveryAcceptanceAssertEquivalent(t, unknownJSONResponse, slowFailure,
		"Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = (SELECT id FROM users WHERE email = $1)`, slowFailureEmail); count != 0 {
		t.Fatalf("slow account-dependent failure retained %d reset tokens", count)
	}
	if tokens := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens`); tokens != baselineTokens {
		t.Fatalf("slow account-dependent failure changed reset token count from %d to %d", baselineTokens, tokens)
	}
	if outbox := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM mail_outbox`); outbox != baselineOutbox {
		t.Fatalf("slow account-dependent failure changed outbox count from %d to %d", baselineOutbox, outbox)
	}
	if jobs := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs`); jobs != baselineMailJobs+baselineCleanupJobs {
		t.Fatalf("slow account-dependent failure changed queued job count to %d", jobs)
	}

	var currentSelector string
	var currentDigest []byte
	var tokenCredentialVersion int64
	if err := db.QueryRow(`SELECT selector, secret_digest, credential_version FROM password_reset_tokens WHERE user_id = (SELECT id FROM users WHERE email = $1)`, knownEmail).Scan(&currentSelector, &currentDigest, &tokenCredentialVersion); err != nil {
		t.Fatalf("read current password reset token: %v", err)
	}
	if len(currentSelector) != 22 || len(currentDigest) != 32 || tokenCredentialVersion != knownVersionBefore {
		t.Fatalf("stored reset token shape selector=%q digest=%d version=%d, want selector and digest only at version %d", currentSelector, len(currentDigest), tokenCredentialVersion, knownVersionBefore)
	}
	if count := jobAcceptanceCount(t, db, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'password_reset_tokens'
		  AND column_name IN ('token', 'secret', 'presented_token')
	`); count != 0 {
		t.Fatalf("password_reset_tokens exposes %d plaintext token columns", count)
	}
	if count := jobAcceptanceCount(t, db, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'mail_outbox'
		  AND column_name IN ('recipient', 'recipients', 'email', 'subject', 'text', 'html', 'body', 'token')
	`); count != 0 {
		t.Fatalf("mail_outbox exposes %d plaintext message columns", count)
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens`); count != 1 {
		t.Fatalf("known/unknown disclosure probe stored %d reset rows, want one current row", count)
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM mail_outbox`); count != 2 {
		t.Fatalf("known/unknown disclosure probe stored %d encrypted messages, want two known-account messages", count)
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.deliver.v1'`); count != 2 {
		t.Fatalf("known/unknown disclosure probe queued %d mail jobs, want two", count)
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.cleanup.v1'`); count != 2 {
		t.Fatalf("known/unknown disclosure probe queued %d cleanup jobs, want two", count)
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.cleanup.v1' AND available_at >= NOW() + INTERVAL '6 days'`); count != 2 {
		t.Fatalf("known/unknown disclosure probe scheduled %d cleanup jobs with bounded delayed retention, want two", count)
	}

	var ciphertexts [][]byte
	rows, err := db.Query(`SELECT ciphertext FROM mail_outbox ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ciphertext []byte
		if err := rows.Scan(&ciphertext); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		ciphertexts = append(ciphertexts, append([]byte(nil), ciphertext...))
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	var queuedPayloads []string
	rows, err = db.Query(`SELECT payload::text FROM goforge_jobs WHERE name IN ('goforge.mail.deliver.v1', 'goforge.mail.cleanup.v1') ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil || len(decoded) != 1 {
			t.Fatalf("mail job payload is not one inspectable field: %v: %s", err, payload)
		}
		outboxID, ok := decoded["outbox_id"].(string)
		if !ok || outboxID == "" {
			t.Fatalf("mail job payload omits opaque outbox_id: %s", payload)
		}
		for _, secret := range []string{knownEmail, unknownEmail, "Reset your password", "/reset-password"} {
			if strings.Contains(payload, secret) {
				t.Fatalf("mail job payload exposed %q: %s", secret, payload)
			}
		}
		queuedPayloads = append(queuedPayloads, payload)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(queuedPayloads) != 4 {
		t.Fatalf("delivery and cleanup queued %d opaque payloads, want four", len(queuedPayloads))
	}

	workerEnvironment := jobAcceptanceEnvironment(applicationEnvironment, map[string]string{
		"MAIL_SMTP_ADDRESS":        smtpServer.address(),
		"MAIL_SMTP_USERNAME":       "",
		"MAIL_SMTP_PASSWORD":       "",
		"MAIL_SMTP_TLS":            "none",
		"MAIL_SMTP_SERVER_NAME":    "localhost",
		"MAIL_SMTP_TIMEOUT":        "2s",
		"JOB_CONCURRENCY":          "1",
		"JOB_POLL_INTERVAL":        "25ms",
		"JOB_LEASE_DURATION":       "2s",
		"JOB_HEARTBEAT_INTERVAL":   "500ms",
		"JOB_OPERATION_TIMEOUT":    "500ms",
		"JOB_CANCELLATION_TIMEOUT": "500ms",
		"JOB_SHUTDOWN_TIMEOUT":     "3s",
	})
	worker := exec.Command(workerBinary)
	worker.Dir = scratch
	worker.Env = workerEnvironment
	configureCommandProcess(worker)
	workerOutput := &synchronizedBuffer{}
	worker.Stdout, worker.Stderr = workerOutput, workerOutput
	if err := worker.Start(); err != nil {
		t.Fatalf("start generated recovery worker: %v", err)
	}
	workerRunning := true
	t.Cleanup(func() {
		if workerRunning {
			stopCommandProcess(t, worker, false)
		}
	})
	jobAcceptanceEventually(t, 10*time.Second, func() (bool, string) {
		return strings.Contains(workerOutput.String(), "job worker_started"), "recovery worker has not reported startup: " + workerOutput.String()
	})

	rejected := recoverySMTPWait(t, smtpServer.rejected, 8*time.Second)
	if rejected.Text == "" || rejected.HTML == "" {
		t.Fatal("temporary SMTP rejection did not receive both password-reset alternatives")
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM mail_outbox`); count < 1 {
		t.Fatal("temporary SMTP rejection removed every durable outbox row")
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.deliver.v1'`); count < 1 {
		t.Fatal("temporary SMTP rejection acknowledged every queued mail job")
	}

	accepted := []recoverySMTPMessage{
		recoverySMTPWait(t, smtpServer.accepted, 8*time.Second),
		recoverySMTPWait(t, smtpServer.accepted, 8*time.Second),
	}
	tokensBySelector := make(map[string]string, 2)
	for _, message := range accepted {
		if len(message.Recipients) != 1 || message.Recipients[0] != knownEmail || message.EnvelopeFrom != "no-reply@example.test" {
			t.Fatalf("password reset envelope = from %q to %v", message.EnvelopeFrom, message.Recipients)
		}
		resetURL, token := recoveryAcceptanceResetLink(t, message, applicationURL)
		if !strings.Contains(message.HTML, resetURL) || !strings.Contains(message.HTML, token) {
			t.Fatalf("HTML alternative omits reset link: %s", message.HTML)
		}
		selector, _, found := strings.Cut(token, ".")
		if !found {
			t.Fatalf("delivered reset token is malformed: %q", token)
		}
		tokensBySelector[selector] = token
	}
	currentToken := tokensBySelector[currentSelector]
	if currentToken == "" || len(tokensBySelector) != 2 {
		t.Fatalf("delivered reset messages do not contain current and superseded selectors: current=%q selectors=%v", currentSelector, tokensBySelector)
	}
	var supersededToken string
	for selector, token := range tokensBySelector {
		if selector != currentSelector {
			supersededToken = token
		}
	}
	for _, plaintext := range []string{knownEmail, currentToken, supersededToken, "Reset your password"} {
		for _, ciphertext := range ciphertexts {
			if bytes.Contains(ciphertext, []byte(plaintext)) {
				t.Fatalf("encrypted outbox exposed %q", plaintext)
			}
		}
		for _, payload := range queuedPayloads {
			if strings.Contains(payload, plaintext) {
				t.Fatalf("queued payload exposed %q: %s", plaintext, payload)
			}
		}
	}
	if bytes.Contains(currentDigest, []byte(strings.TrimPrefix(currentToken, currentSelector+"."))) || bytes.Contains(currentDigest, []byte(currentToken)) {
		t.Fatal("password_reset_tokens persisted the presented token or secret")
	}
	jobAcceptanceEventually(t, 12*time.Second, func() (bool, string) {
		outbox := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM mail_outbox`)
		jobs := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM goforge_jobs WHERE name = 'goforge.mail.deliver.v1'`)
		return outbox == 0 && jobs == 0, fmt.Sprintf("waiting for accepted mail cleanup: outbox=%d jobs=%d", outbox, jobs)
	})

	const replacementPassword = "a replacement password with enough entropy"
	resetBody := func(token, password string) string {
		return fmt.Sprintf(`{"token":%q,"password":%q,"password_confirmation":%q}`, token, password, password)
	}
	superseded := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 5 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset", resetBody(supersededToken, replacementPassword), "192.0.2.20")
	if superseded.Status != http.StatusUnprocessableEntity || !strings.Contains(superseded.Body, "invalid or has expired") {
		t.Fatalf("superseded reset response = %d: %s", superseded.Status, superseded.Body)
	}
	recoveryAcceptanceAssertPrivateHeaders(t, superseded)
	malformedToken := "not-a-token"
	malformed := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 5 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset", resetBody(malformedToken, replacementPassword), "192.0.2.21")
	recoveryAcceptanceAssertEquivalent(t, superseded, malformed, "Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")

	expiredForgot := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 3 * time.Second}, http.MethodPost, baseURL+"/auth/password/forgot", forgotBody(expiredEmail), "192.0.2.22")
	if expiredForgot.Status != http.StatusAccepted {
		t.Fatalf("request expiring token: %d: %s", expiredForgot.Status, expiredForgot.Body)
	}
	expiredMessage := recoverySMTPWait(t, smtpServer.accepted, 8*time.Second)
	_, expiredToken := recoveryAcceptanceResetLink(t, expiredMessage, applicationURL)
	_, expiredSecret, ok := strings.Cut(expiredToken, ".")
	if !ok {
		t.Fatalf("expired-account reset token is malformed: %q", expiredToken)
	}
	hybridToken := currentSelector + "." + expiredSecret
	hybrid := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 5 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset", resetBody(hybridToken, replacementPassword), "192.0.2.30")
	recoveryAcceptanceAssertEquivalent(t, superseded, hybrid, "Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")
	if _, err := db.Exec(`
		UPDATE password_reset_tokens
		SET created_at = NOW() - INTERVAL '2 hours', expires_at = NOW() - INTERVAL '1 hour', updated_at = NOW() - INTERVAL '2 hours'
		WHERE user_id = (SELECT id FROM users WHERE email = $1)
	`, expiredEmail); err != nil {
		t.Fatalf("expire reset token: %v", err)
	}
	expired := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 5 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset", resetBody(expiredToken, replacementPassword), "192.0.2.23")
	recoveryAcceptanceAssertEquivalent(t, superseded, expired, "Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")

	valid := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 8 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset", resetBody(currentToken, replacementPassword), "192.0.2.24")
	if valid.Status != http.StatusNoContent || valid.Body != "" {
		t.Fatalf("valid password reset = %d: %s", valid.Status, valid.Body)
	}
	recoveryAcceptanceAssertPrivateHeaders(t, valid)
	if got := recoveryAcceptanceCredentialVersion(t, db, knownEmail); got != knownVersionBefore+1 {
		t.Fatalf("password reset credential_version=%d, want %d", got, knownVersionBefore+1)
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = (SELECT id FROM users WHERE email = $1)`, knownEmail); count != 0 {
		t.Fatalf("valid password reset left %d reusable token rows", count)
	}
	replay := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 5 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset", resetBody(currentToken, replacementPassword), "192.0.2.25")
	recoveryAcceptanceAssertEquivalent(t, superseded, replay, "Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")
	if response := recoveryAcceptanceMustJSON(t, knownSession, http.MethodGet, baseURL+"/auth/me", "", "192.0.2.26"); response.Status != http.StatusUnauthorized {
		t.Fatalf("pre-reset session survived credential generation change: %d: %s", response.Status, response.Body)
	}
	oldLogin := recoveryAcceptanceMustJSON(t, clientWithCookies(t), http.MethodPost, baseURL+"/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, knownEmail, oldPassword), "192.0.2.27")
	if oldLogin.Status != http.StatusUnauthorized {
		t.Fatalf("old password still authenticates: %d: %s", oldLogin.Status, oldLogin.Body)
	}
	newLogin := recoveryAcceptanceMustJSON(t, clientWithCookies(t), http.MethodPost, baseURL+"/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, knownEmail, replacementPassword), "192.0.2.28")
	if newLogin.Status != http.StatusOK {
		t.Fatalf("replacement password does not authenticate: %d: %s", newLogin.Status, newLogin.Body)
	}

	concurrentVersionBeforeRevocation := recoveryAcceptanceCredentialVersion(t, db, concurrentEmail)
	concurrentForgot := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 3 * time.Second}, http.MethodPost, baseURL+"/auth/password/forgot", forgotBody(concurrentEmail), "192.0.2.29")
	if concurrentForgot.Status != http.StatusAccepted {
		t.Fatalf("request concurrent token: %d: %s", concurrentForgot.Status, concurrentForgot.Body)
	}
	concurrentMessage := recoverySMTPWait(t, smtpServer.accepted, 8*time.Second)
	_, invalidatedConcurrentToken := recoveryAcceptanceResetLink(t, concurrentMessage, applicationURL)
	revoked := recoveryAcceptanceMustJSON(t, concurrentSession, http.MethodPost, baseURL+"/auth/logout-all", `{}`, "192.0.2.31")
	if revoked.Status != http.StatusNoContent {
		t.Fatalf("revoke sessions before reset = %d: %s", revoked.Status, revoked.Body)
	}
	if got := recoveryAcceptanceCredentialVersion(t, db, concurrentEmail); got != concurrentVersionBeforeRevocation+1 {
		t.Fatalf("credential revocation version=%d, want %d", got, concurrentVersionBeforeRevocation+1)
	}
	invalidated := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 5 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset", resetBody(invalidatedConcurrentToken, replacementPassword), "192.0.2.32")
	recoveryAcceptanceAssertEquivalent(t, superseded, invalidated, "Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")

	concurrentForgot = recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 3 * time.Second}, http.MethodPost, baseURL+"/auth/password/forgot", forgotBody(concurrentEmail), "192.0.2.33")
	if concurrentForgot.Status != http.StatusAccepted {
		t.Fatalf("request post-revocation concurrent token: %d: %s", concurrentForgot.Status, concurrentForgot.Body)
	}
	concurrentMessage = recoverySMTPWait(t, smtpServer.accepted, 8*time.Second)
	_, concurrentToken := recoveryAcceptanceResetLink(t, concurrentMessage, applicationURL)
	concurrentVersionBefore := recoveryAcceptanceCredentialVersion(t, db, concurrentEmail)
	const concurrentPassword = "a concurrent replacement password"
	type resetResult struct {
		response recoveryAcceptanceResponse
		err      error
	}
	results := make(chan resetResult, 2)
	var group sync.WaitGroup
	for index := range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			response, requestErr := recoveryAcceptanceJSON(&http.Client{Timeout: 10 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset", resetBody(concurrentToken, concurrentPassword), fmt.Sprintf("198.18.0.%d", index+1))
			results <- resetResult{response: response, err: requestErr}
		}()
	}
	group.Wait()
	close(results)
	winners, losers := 0, 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent reset request: %v", result.err)
		}
		switch result.response.Status {
		case http.StatusNoContent:
			winners++
		case http.StatusUnprocessableEntity:
			losers++
			recoveryAcceptanceAssertEquivalent(t, superseded, result.response, "Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")
		default:
			t.Fatalf("concurrent reset status=%d: %s", result.response.Status, result.response.Body)
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("concurrent reset winners=%d losers=%d, want exactly one each", winners, losers)
	}
	if got := recoveryAcceptanceCredentialVersion(t, db, concurrentEmail); got != concurrentVersionBefore+1 {
		t.Fatalf("concurrent reset credential_version=%d, want %d", got, concurrentVersionBefore+1)
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = (SELECT id FROM users WHERE email = $1)`, concurrentEmail); count != 0 {
		t.Fatalf("concurrent reset left %d token rows", count)
	}

	resetFailureVersion := recoveryAcceptanceCredentialVersion(t, db, resetFailureEmail)
	resetFailureForgot := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 3 * time.Second}, http.MethodPost, baseURL+"/auth/password/forgot", forgotBody(resetFailureEmail), "203.0.113.60")
	if resetFailureForgot.Status != http.StatusAccepted {
		t.Fatalf("request reset-rollback token: %d: %s", resetFailureForgot.Status, resetFailureForgot.Body)
	}
	resetFailureMessage := recoverySMTPWait(t, smtpServer.accepted, 8*time.Second)
	_, resetFailureToken := recoveryAcceptanceResetLink(t, resetFailureMessage, applicationURL)
	restoreReset := recoveryAcceptanceRejectResetUpdate(t, db)
	resetFailure := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 8 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset",
		resetBody(resetFailureToken, replacementPassword), "203.0.113.61")
	restoreReset()
	if resetFailure.Status != http.StatusInternalServerError || strings.Contains(resetFailure.Body, resetFailureToken) || strings.Contains(resetFailure.Body, replacementPassword) {
		t.Fatalf("injected reset failure response = %d: %s", resetFailure.Status, resetFailure.Body)
	}
	recoveryAcceptanceAssertPrivateHeaders(t, resetFailure)
	if got := recoveryAcceptanceCredentialVersion(t, db, resetFailureEmail); got != resetFailureVersion {
		t.Fatalf("failed reset changed credential version to %d, want %d", got, resetFailureVersion)
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = (SELECT id FROM users WHERE email = $1)`, resetFailureEmail); count != 1 {
		t.Fatalf("failed reset consumed token rows=%d", count)
	}
	resetRetry := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 8 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset",
		resetBody(resetFailureToken, replacementPassword), "203.0.113.62")
	if resetRetry.Status != http.StatusNoContent {
		t.Fatalf("reset retry after rollback = %d: %s", resetRetry.Status, resetRetry.Body)
	}
	if got := recoveryAcceptanceCredentialVersion(t, db, resetFailureEmail); got != resetFailureVersion+1 {
		t.Fatalf("reset retry credential version=%d, want %d", got, resetFailureVersion+1)
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = (SELECT id FROM users WHERE email = $1)`, resetFailureEmail); count != 0 {
		t.Fatalf("reset retry retained token rows=%d", count)
	}

	rateVersionBefore := recoveryAcceptanceCredentialVersion(t, db, rateLimitEmail)
	rateForgot := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 3 * time.Second}, http.MethodPost, baseURL+"/auth/password/forgot", forgotBody(rateLimitEmail), "203.0.113.40")
	if rateForgot.Status != http.StatusAccepted {
		t.Fatalf("request rate-limit token: %d: %s", rateForgot.Status, rateForgot.Body)
	}
	rateMessage := recoverySMTPWait(t, smtpServer.accepted, 8*time.Second)
	_, rateToken := recoveryAcceptanceResetLink(t, rateMessage, applicationURL)
	rateSelector, rateSecret, ok := strings.Cut(rateToken, ".")
	if !ok || rateSecret == "" {
		t.Fatalf("rate-limit reset token is malformed: %q", rateToken)
	}
	replacementByte := byte('A')
	if rateSecret[0] == replacementByte {
		replacementByte = 'B'
	}
	wrongRateToken := rateSelector + "." + string(replacementByte) + rateSecret[1:]
	for attempt := range 3 {
		response := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 5 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset",
			resetBody(wrongRateToken, replacementPassword), fmt.Sprintf("203.0.113.%d", 41+attempt))
		recoveryAcceptanceAssertEquivalent(t, superseded, response, "Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")
	}
	rateBlocked := recoveryAcceptanceMustJSON(t, &http.Client{Timeout: 5 * time.Second}, http.MethodPost, baseURL+"/auth/password/reset",
		resetBody(rateToken, replacementPassword), "203.0.113.44")
	recoveryAcceptanceAssertEquivalent(t, superseded, rateBlocked, "Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag")
	if got := recoveryAcceptanceCredentialVersion(t, db, rateLimitEmail); got != rateVersionBefore {
		t.Fatalf("selector limiter allowed credential change: version=%d want=%d", got, rateVersionBefore)
	}
	if count := jobAcceptanceCount(t, db, `SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = (SELECT id FROM users WHERE email = $1)`, rateLimitEmail); count != 1 {
		t.Fatalf("selector limiter consumed valid token after exhaustion: rows=%d", count)
	}

	browserVersionBefore := recoveryAcceptanceCredentialVersion(t, db, browserEmail)
	browserRecovery := clientWithCookiesNoRedirect(t)
	browserForgotForm := recoveryAcceptanceMustBrowser(t, browserRecovery, http.MethodGet, baseURL+"/forgot-password", nil, "203.0.113.50")
	if browserForgotForm.Status != http.StatusOK {
		t.Fatalf("browser recovery form = %d: %s", browserForgotForm.Status, browserForgotForm.Body)
	}
	browserForgot := recoveryAcceptanceMustBrowser(t, browserRecovery, http.MethodPost, baseURL+"/forgot-password", url.Values{
		"_token": {browserCSRF(t, browserForgotForm.Body)}, "email": {browserEmail},
	}, "203.0.113.51")
	if browserForgot.Status != http.StatusSeeOther || browserForgot.Header.Get("Location") != "/forgot-password" {
		t.Fatalf("browser recovery request = %d %q: %s", browserForgot.Status, browserForgot.Header.Get("Location"), browserForgot.Body)
	}
	browserMessage := recoverySMTPWait(t, smtpServer.accepted, 8*time.Second)
	_, browserToken := recoveryAcceptanceResetLink(t, browserMessage, applicationURL)
	browserResetForm := recoveryAcceptanceMustBrowser(t, browserRecovery, http.MethodGet,
		baseURL+"/reset-password?token="+url.QueryEscape(browserToken), nil, "203.0.113.52")
	if browserResetForm.Status != http.StatusOK {
		t.Fatalf("browser reset form = %d: %s", browserResetForm.Status, browserResetForm.Body)
	}
	const browserPassword = "a browser replacement password"
	browserReset := recoveryAcceptanceMustBrowser(t, browserRecovery, http.MethodPost, baseURL+"/reset-password", url.Values{
		"_token": {browserCSRF(t, browserResetForm.Body)}, "token": {browserToken},
		"password": {browserPassword}, "password_confirmation": {browserPassword},
	}, "203.0.113.53")
	if browserReset.Status != http.StatusSeeOther || browserReset.Header.Get("Location") != "/login" {
		t.Fatalf("browser reset = %d %q: %s", browserReset.Status, browserReset.Header.Get("Location"), browserReset.Body)
	}
	recoveryAcceptanceAssertPrivateHeaders(t, browserReset)
	if got := recoveryAcceptanceCredentialVersion(t, db, browserEmail); got != browserVersionBefore+1 {
		t.Fatalf("browser reset credential_version=%d, want %d", got, browserVersionBefore+1)
	}
	if response := recoveryAcceptanceMustJSON(t, browserSession, http.MethodGet, baseURL+"/auth/me", "", "203.0.113.54"); response.Status != http.StatusUnauthorized {
		t.Fatalf("browser account pre-reset session survived: %d: %s", response.Status, response.Body)
	}
	if response := recoveryAcceptanceMustJSON(t, clientWithCookies(t), http.MethodPost, baseURL+"/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, browserEmail, oldPassword), "203.0.113.55"); response.Status != http.StatusUnauthorized {
		t.Fatalf("browser account old password still authenticates: %d: %s", response.Status, response.Body)
	}
	if response := recoveryAcceptanceMustJSON(t, clientWithCookies(t), http.MethodPost, baseURL+"/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, browserEmail, browserPassword), "203.0.113.56"); response.Status != http.StatusOK {
		t.Fatalf("browser account replacement password does not authenticate: %d: %s", response.Status, response.Body)
	}

	secrets := []string{knownEmail, expiredEmail, concurrentEmail, browserEmail, rateLimitEmail, resetFailureEmail, slowFailureEmail, currentToken, expiredToken, concurrentToken, invalidatedConcurrentToken, supersededToken, hybridToken, browserToken, rateToken, wrongRateToken, resetFailureToken}
	secrets = append(secrets, failureEmails...)
	for _, secret := range secrets {
		if strings.Contains(workerOutput.String(), secret) || strings.Contains(serverOutput.String(), secret) {
			t.Fatalf("generated process logs exposed recovery secret %q", secret)
		}
	}
	stopCommandProcess(t, worker, true)
	workerRunning = false
	stopCommandProcess(t, server, true)
	serverRunning = false
}

func recoveryAcceptanceRejectInsert(t *testing.T, db *sql.DB, stage string) func() {
	t.Helper()
	var table, condition string
	switch stage {
	case "token":
		table = "password_reset_tokens"
	case "outbox":
		table = "mail_outbox"
	case "job":
		table = "goforge_jobs"
		condition = " WHEN (NEW.name = 'goforge.mail.cleanup.v1')"
	default:
		t.Fatalf("unsupported recovery failure stage %q", stage)
	}
	function := "goforge_recovery_reject_" + stage
	trigger := function + "_insert"
	if _, err := db.Exec(`CREATE FUNCTION ` + function + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected recovery persistence failure'; END $$`); err != nil {
		t.Fatalf("create %s failure function: %v", stage, err)
	}
	if _, err := db.Exec(`CREATE TRIGGER ` + trigger + ` BEFORE INSERT ON ` + table + ` FOR EACH ROW` + condition + ` EXECUTE FUNCTION ` + function + `()`); err != nil {
		_, _ = db.Exec(`DROP FUNCTION ` + function + `()`)
		t.Fatalf("create %s failure trigger: %v", stage, err)
	}
	var once sync.Once
	restore := func() {
		once.Do(func() {
			if _, err := db.Exec(`DROP TRIGGER ` + trigger + ` ON ` + table); err != nil {
				t.Errorf("drop %s failure trigger: %v", stage, err)
			}
			if _, err := db.Exec(`DROP FUNCTION ` + function + `()`); err != nil {
				t.Errorf("drop %s failure function: %v", stage, err)
			}
		})
	}
	t.Cleanup(restore)
	return restore
}

func recoveryAcceptanceRejectResetUpdate(t *testing.T, db *sql.DB) func() {
	t.Helper()
	const function = "goforge_recovery_reject_reset"
	const trigger = "goforge_recovery_reject_reset_update"
	if _, err := db.Exec(`CREATE FUNCTION ` + function + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected reset update failure'; END $$`); err != nil {
		t.Fatalf("create reset failure function: %v", err)
	}
	if _, err := db.Exec(`CREATE TRIGGER ` + trigger + ` BEFORE UPDATE OF password_hash ON users FOR EACH ROW WHEN (OLD.password_hash IS DISTINCT FROM NEW.password_hash) EXECUTE FUNCTION ` + function + `()`); err != nil {
		_, _ = db.Exec(`DROP FUNCTION ` + function + `()`)
		t.Fatalf("create reset failure trigger: %v", err)
	}
	var once sync.Once
	restore := func() {
		once.Do(func() {
			if _, err := db.Exec(`DROP TRIGGER ` + trigger + ` ON users`); err != nil {
				t.Errorf("drop reset failure trigger: %v", err)
			}
			if _, err := db.Exec(`DROP FUNCTION ` + function + `()`); err != nil {
				t.Errorf("drop reset failure function: %v", err)
			}
		})
	}
	t.Cleanup(restore)
	return restore
}

func recoveryAcceptanceDelayTokenInsert(t *testing.T, db *sql.DB) func() {
	t.Helper()
	const function = "goforge_recovery_delay_token"
	const trigger = "goforge_recovery_delay_token_insert"
	if _, err := db.Exec(`CREATE FUNCTION ` + function + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(2); RETURN NEW; END $$`); err != nil {
		t.Fatalf("create slow token function: %v", err)
	}
	if _, err := db.Exec(`CREATE TRIGGER ` + trigger + ` BEFORE INSERT ON password_reset_tokens FOR EACH ROW EXECUTE FUNCTION ` + function + `()`); err != nil {
		_, _ = db.Exec(`DROP FUNCTION ` + function + `()`)
		t.Fatalf("create slow token trigger: %v", err)
	}
	var once sync.Once
	restore := func() {
		once.Do(func() {
			if _, err := db.Exec(`DROP TRIGGER ` + trigger + ` ON password_reset_tokens`); err != nil {
				t.Errorf("drop slow token trigger: %v", err)
			}
			if _, err := db.Exec(`DROP FUNCTION ` + function + `()`); err != nil {
				t.Errorf("drop slow token function: %v", err)
			}
		})
	}
	t.Cleanup(restore)
	return restore
}

func recoveryAcceptanceCredentialVersion(t *testing.T, db *sql.DB, email string) int64 {
	t.Helper()
	var version int64
	if err := db.QueryRow(`SELECT credential_version FROM users WHERE email = $1`, email).Scan(&version); err != nil {
		t.Fatalf("read credential version for %s: %v", email, err)
	}
	return version
}

func recoveryAcceptanceResetLink(t *testing.T, message recoverySMTPMessage, applicationURL string) (string, string) {
	t.Helper()
	for _, line := range strings.Split(message.Text, "\n") {
		candidate := strings.TrimSpace(line)
		if !strings.HasPrefix(candidate, applicationURL+"/reset-password?") {
			continue
		}
		parsed, err := url.Parse(candidate)
		if err != nil {
			t.Fatalf("parse delivered reset URL: %v", err)
		}
		if parsed.Scheme+"://"+parsed.Host != applicationURL || parsed.Path != "/reset-password" {
			t.Fatalf("reset URL used request-controlled origin: %s", candidate)
		}
		token := parsed.Query().Get("token")
		if token == "" {
			t.Fatalf("reset URL omits token: %s", candidate)
		}
		return candidate, token
	}
	t.Fatalf("text alternative omits configured reset URL: %s", message.Text)
	return "", ""
}
