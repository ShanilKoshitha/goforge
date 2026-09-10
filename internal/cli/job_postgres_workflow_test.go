package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestGeneratedJobPostgresWorkflow(t *testing.T) {
	started := time.Now()
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}

	root := projectRoot(t)
	scratch := t.TempDir()
	forgeBinary := filepath.Join(scratch, "forge")
	workerBinary := filepath.Join(scratch, "job-worker")
	helperBinary := filepath.Join(scratch, "job-acceptance")
	if runtime.GOOS == "windows" {
		forgeBinary += ".exe"
		workerBinary += ".exe"
		helperBinary += ".exe"
	}
	baseEnvironment := append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := generatedCommand(root, baseEnvironment, "go", "build", "-o", forgeBinary, "./cmd/forge"); err != nil {
		t.Fatalf("build forge CLI: %v\n%s", err, output)
	}

	directory := filepath.Join(scratch, "jobboard")
	if output, err := generatedCommand(scratch, baseEnvironment, forgeBinary, "new", "jobboard", "--module", "example.com/jobboard", "--replace", root); err != nil {
		t.Fatalf("forge new: %v\n%s", err, output)
	}
	manifest, err := os.ReadFile(filepath.Join(directory, "forge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte("version: 12")) {
		t.Fatalf("fresh application is not format 12:\n%s", manifest)
	}
	for _, name := range []string{"RecordEffect", "PoisonMessage"} {
		if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "make:job", name); err != nil {
			t.Fatalf("forge make:job %s: %v\n%s", name, err, output)
		}
	}

	registry, err := os.ReadFile(filepath.Join(directory, "internal", "jobs", "registry_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if poison, record := bytes.Index(registry, []byte("PoisonMessageDefinition")), bytes.Index(registry, []byte("RecordEffectDefinition")); poison < 0 || record < 0 || poison > record {
		t.Fatalf("generated registry is incomplete or nondeterministic:\n%s", registry)
	}
	metadata, err := os.ReadFile(filepath.Join(directory, ".forge", "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state jobState
	if err := json.Unmarshal(metadata, &state); err != nil {
		t.Fatalf("decode generated job metadata: %v", err)
	}
	if len(state.Jobs) != 2 || state.Jobs[0] != (jobSpec{Name: "PoisonMessage", File: "poison_message", Definition: "poison_message.v1"}) || state.Jobs[1] != (jobSpec{Name: "RecordEffect", File: "record_effect", Definition: "record_effect.v1"}) {
		t.Fatalf("generated job metadata = %#v", state.Jobs)
	}
	for path, declarations := range map[string][]string{
		"poison_message.go": {"type PoisonMessage struct{}", `job.MustDefine[PoisonMessage]("poison_message.v1"`, "func DispatchPoisonMessage("},
		"record_effect.go":  {"type RecordEffect struct{}", `job.MustDefine[RecordEffect]("record_effect.v1"`, "func DispatchRecordEffect("},
	} {
		contents, readErr := os.ReadFile(filepath.Join(directory, "internal", "jobs", path))
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, declaration := range declarations {
			if !bytes.Contains(contents, []byte(declaration)) {
				t.Fatalf("generated %s omits %q:\n%s", path, declaration, contents)
			}
		}
	}

	fixtures := map[string]string{
		filepath.Join("internal", "jobs", "record_effect.go"):                     jobAcceptanceRecordEffectSource,
		filepath.Join("internal", "jobs", "record_effect_test.go"):                jobAcceptanceRecordEffectTestSource,
		filepath.Join("internal", "jobs", "poison_message.go"):                    jobAcceptancePoisonMessageSource,
		filepath.Join("internal", "jobs", "poison_message_test.go"):               jobAcceptancePoisonMessageTestSource,
		filepath.Join("database", "migrations", "900001_job_acceptance.up.sql"):   jobAcceptanceMigrationUp,
		filepath.Join("database", "migrations", "900001_job_acceptance.down.sql"): jobAcceptanceMigrationDown,
		filepath.Join(".forge", "job_acceptance.go"):                              jobAcceptanceHelperSource,
	}
	for relative, contents := range fixtures {
		path := filepath.Join(directory, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write generated application fixture %s: %v", relative, err)
		}
	}
	if output, err := generatedCommand(directory, baseEnvironment, "gofmt", "-w",
		filepath.Join("internal", "jobs", "record_effect.go"),
		filepath.Join("internal", "jobs", "record_effect_test.go"),
		filepath.Join("internal", "jobs", "poison_message.go"),
		filepath.Join("internal", "jobs", "poison_message_test.go"),
		filepath.Join(".forge", "job_acceptance.go")); err != nil {
		t.Fatalf("gofmt application-owned job fixtures: %v\n%s", err, output)
	}

	schema := fmt.Sprintf("goforge_job_acceptance_%d", time.Now().UnixNano())
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
	applicationEnvironment := jobAcceptanceEnvironment(baseEnvironment, map[string]string{
		"DATABASE_URL": isolatedURL,
		"APP_ENV":      "local",
	})
	if output, err := generatedCommand(directory, applicationEnvironment, forgeBinary, "migrate"); err != nil {
		t.Fatalf("migrate isolated job schema: %v\n%s", err, output)
	} else if !strings.Contains(output, "900001_job_acceptance") {
		t.Fatalf("application-owned job migration was not applied:\n%s", output)
	}
	for _, gate := range [][]string{{"test", "./..."}, {"vet", "./..."}, {"test", "-race", "./..."}} {
		if output, err := generatedCommand(directory, applicationEnvironment, "go", gate...); err != nil {
			t.Fatalf("fresh generated application go %s: %v\n%s", strings.Join(gate, " "), err, output)
		}
	}
	if output, err := generatedCommand(directory, applicationEnvironment, "go", "build", "-o", workerBinary, "./cmd/worker"); err != nil {
		t.Fatalf("build generated worker: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, applicationEnvironment, "go", "build", "-o", helperBinary, "./.forge/job_acceptance.go"); err != nil {
		t.Fatalf("build generated dispatch helper: %v\n%s", err, output)
	}

	runDirectory := filepath.Join(scratch, "outside-source-cwd")
	if err := os.MkdirAll(runDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	seedOutput, err := generatedCommand(runDirectory, applicationEnvironment, helperBinary, "seed")
	if err != nil {
		t.Fatalf("dispatch typed job acceptance fixtures: %v\n%s", err, seedOutput)
	}
	var dispatched jobAcceptanceDispatches
	if err := json.Unmarshal([]byte(seedOutput), &dispatched); err != nil {
		t.Fatalf("decode dispatch evidence: %v\n%s", err, seedOutput)
	}
	if dispatched.Direct == "" || dispatched.Committed == "" || dispatched.RolledBack == "" || dispatched.PanickedRollback == "" || !dispatched.PanicRecovered || dispatched.Delayed == "" || dispatched.AbsoluteAt == "" || dispatched.AbsoluteAvailable.IsZero() || dispatched.Reclaim == "" || len(dispatched.Failed) != 2 {
		t.Fatalf("dispatch helper returned incomplete evidence: %+v", dispatched)
	}
	if !dispatched.DedupFirstEnqueued || dispatched.DedupSecondEnqueued || dispatched.DedupFirst == "" || dispatched.DedupFirst != dispatched.DedupSecond {
		t.Fatalf("active deduplication evidence = %+v", dispatched)
	}

	db, err := sql.Open("pgx", isolatedURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs WHERE id = $1::uuid", dispatched.RolledBack); got != 0 {
		t.Fatalf("database.Transaction rollback left %d queued rows", got)
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs WHERE id = $1::uuid", dispatched.PanickedRollback); got != 0 {
		t.Fatalf("panicking database.Transaction callback left %d queued rows", got)
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs WHERE id = $1::uuid", dispatched.Committed); got != 1 {
		t.Fatalf("database.Transaction commit queued %d rows, want 1", got)
	}
	var delayedAvailable time.Time
	if err := db.QueryRow("SELECT available_at FROM goforge_jobs WHERE id = $1::uuid", dispatched.Delayed).Scan(&delayedAvailable); err != nil {
		t.Fatalf("read delayed job availability: %v", err)
	}
	if until := time.Until(delayedAvailable); until < 2*time.Second {
		t.Fatalf("one-off delay was not durably snapshotted: available in %s", until)
	}
	var absoluteAvailable time.Time
	if err := db.QueryRow("SELECT available_at FROM goforge_jobs WHERE id = $1::uuid", dispatched.AbsoluteAt).Scan(&absoluteAvailable); err != nil {
		t.Fatalf("read absolute job availability: %v", err)
	}
	if delta := absoluteAvailable.Sub(dispatched.AbsoluteAvailable); delta < -time.Millisecond || delta > time.Millisecond {
		t.Fatalf("job.At timestamp changed across dispatch: database=%s requested=%s delta=%s", absoluteAvailable.Format(time.RFC3339Nano), dispatched.AbsoluteAvailable.Format(time.RFC3339Nano), delta)
	}
	if until := time.Until(absoluteAvailable); until < 3*time.Second {
		t.Fatalf("absolute job.At was not a real future timestamp: available in %s", until)
	}

	workerEnvironment := jobAcceptanceEnvironment(applicationEnvironment, map[string]string{
		"JOB_QUEUES":             "default",
		"JOB_CONCURRENCY":        "2",
		"JOB_POLL_INTERVAL":      "25ms",
		"JOB_LEASE_DURATION":     "2s",
		"JOB_HEARTBEAT_INTERVAL": "500ms",
		"JOB_OPERATION_TIMEOUT":  "500ms",
		"JOB_SHUTDOWN_TIMEOUT":   "3s",
		"MAIL_SMTP_ADDRESS":      "127.0.0.1:1",
		"MAIL_SMTP_TLS":          "none",
		"MAIL_SMTP_SERVER_NAME":  "localhost",
		"MAIL_SMTP_TIMEOUT":      "1s",
		"MAIL_OUTBOX_KEY":        base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x6b}, 32)),
	})
	for _, entry := range workerEnvironment {
		if strings.HasPrefix(strings.ToUpper(entry), "SESSION_SECRET=") {
			t.Fatal("worker acceptance environment unexpectedly contains SESSION_SECRET")
		}
	}
	workers := make([]*exec.Cmd, 0, 2)
	workerOutputs := make([]*synchronizedBuffer, 0, 2)
	workersRunning := true
	for index := range 2 {
		command := exec.Command(workerBinary)
		command.Dir = runDirectory
		command.Env = workerEnvironment
		configureCommandProcess(command)
		output := &synchronizedBuffer{}
		command.Stdout, command.Stderr = output, output
		if err := command.Start(); err != nil {
			t.Fatalf("start generated worker %d: %v", index+1, err)
		}
		workers = append(workers, command)
		workerOutputs = append(workerOutputs, output)
	}
	t.Cleanup(func() {
		if workersRunning {
			for _, worker := range workers {
				stopCommandProcess(t, worker, false)
			}
		}
	})
	jobAcceptanceEventually(t, 10*time.Second, func() (bool, string) {
		for index, output := range workerOutputs {
			if workers[index].ProcessState != nil {
				return false, fmt.Sprintf("worker %d exited early: %s", index+1, output.String())
			}
			if !strings.Contains(output.String(), "job worker_started") {
				return false, fmt.Sprintf("worker %d has not reported startup", index+1)
			}
		}
		return true, ""
	})

	immediateKeys := []string{"direct-db", "transaction-commit", "active-dedup"}
	jobAcceptanceEventually(t, 12*time.Second, func() (bool, string) {
		for _, key := range immediateKeys {
			if jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM job_effects WHERE business_key = $1", key) != 1 {
				return false, "waiting for immediate effect " + key
			}
		}
		return true, ""
	})
	if !time.Now().Before(delayedAvailable) {
		t.Fatalf("immediate jobs did not finish before delayed job became eligible (available_at=%s)", delayedAvailable.Format(time.RFC3339Nano))
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM job_effects WHERE business_key = $1", "one-off-delay"); got != 0 {
		t.Fatalf("delayed job produced %d effects before available_at", got)
	}
	if !time.Now().Before(absoluteAvailable) {
		t.Fatalf("immediate jobs did not finish before job.At became eligible (available_at=%s)", absoluteAvailable.Format(time.RFC3339Nano))
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM job_effects WHERE business_key = $1", "absolute-at"); got != 0 {
		t.Fatalf("job.At produced %d effects before its absolute timestamp", got)
	}
	jobAcceptanceEventually(t, 12*time.Second, func() (bool, string) {
		if jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM job_effects WHERE business_key = $1", "one-off-delay") != 1 {
			return false, "waiting for delayed effect"
		}
		if jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM job_effects WHERE business_key = $1", "absolute-at") != 1 {
			return false, "waiting for absolute job.At effect"
		}
		if jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_failed_jobs") != 2 {
			return false, "waiting for two bounded failures"
		}
		return true, ""
	})

	for _, item := range dispatched.Failed {
		var attempts, maxAttempts int
		var failureMessage string
		if err := db.QueryRow("SELECT attempts, max_attempts, failure_message FROM goforge_failed_jobs WHERE id = $1::uuid", item.ID).Scan(&attempts, &maxAttempts, &failureMessage); err != nil {
			t.Fatalf("read failed job %s: %v", item.ID, err)
		}
		if attempts != 2 || maxAttempts != 2 {
			t.Fatalf("failed job %s attempts=%d/%d, want 2/2", item.ID, attempts, maxAttempts)
		}
		if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs WHERE id = $1::uuid", item.ID); got != 0 {
			t.Fatalf("terminal job %s remains active", item.ID)
		}
		if failureMessage != "handler returned an error" {
			t.Fatalf("failed job %s persisted unsafe diagnostic %q", item.ID, failureMessage)
		}
	}

	failedOutput, err := generatedCommand(directory, applicationEnvironment, forgeBinary, "queue:failed")
	if err != nil {
		t.Fatalf("live forge queue:failed: %v\n%s", err, failedOutput)
	}
	for _, item := range dispatched.Failed {
		if !strings.Contains(failedOutput, item.ID) {
			t.Errorf("queue:failed omits %s:\n%s", item.ID, failedOutput)
		}
		if strings.Contains(failedOutput, item.Secret) {
			t.Errorf("queue:failed exposed payload secret %q:\n%s", item.Secret, failedOutput)
		}
	}
	if strings.Contains(failedOutput, `forced failure`) || strings.Count(failedOutput, `message="handler returned an error"`) != 2 || strings.Count(strings.TrimSpace(failedOutput), "\n") != 1 {
		t.Fatalf("queue:failed did not keep two payload-free, sanitized records on one line each:\n%s", failedOutput)
	}

	retry := dispatched.Failed[0]
	if output, err := generatedCommand(runDirectory, applicationEnvironment, helperBinary, "allow", retry.BusinessKey); err != nil {
		t.Fatalf("make retry handler succeed: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, applicationEnvironment, forgeBinary, "queue:retry", retry.ID); err != nil {
		t.Fatalf("live forge queue:retry %s: %v\n%s", retry.ID, err, output)
	} else if !strings.Contains(output, "queue:retry "+retry.ID) {
		t.Fatalf("queue:retry did not confirm per-ID change:\n%s", output)
	}
	jobAcceptanceEventually(t, 10*time.Second, func() (bool, string) {
		if jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM job_effects WHERE business_key = $1", retry.BusinessKey) != 1 {
			return false, "waiting for retried effect"
		}
		if jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs WHERE id = $1::uuid", retry.ID) != 0 {
			return false, "waiting for retried job acknowledgement"
		}
		return true, ""
	})
	var retryAttempt int
	if err := db.QueryRow("SELECT attempt FROM job_effects WHERE business_key = $1", retry.BusinessKey).Scan(&retryAttempt); err != nil || retryAttempt != 1 {
		t.Fatalf("retried job attempt after administrative reset = %d, err=%v, want 1", retryAttempt, err)
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_failed_jobs WHERE id = $1::uuid", retry.ID); got != 0 {
		t.Fatalf("retried job remains in failed_jobs: %d", got)
	}

	forgotten := dispatched.Failed[1]
	if output, err := generatedCommand(directory, applicationEnvironment, forgeBinary, "queue:forget", forgotten.ID); err != nil {
		t.Fatalf("live forge queue:forget %s: %v\n%s", forgotten.ID, err, output)
	} else if !strings.Contains(output, "queue:forget "+forgotten.ID) {
		t.Fatalf("queue:forget did not confirm per-ID change:\n%s", output)
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_failed_jobs WHERE id = $1::uuid", forgotten.ID); got != 0 {
		t.Fatalf("forgotten job remains in failed_jobs: %d", got)
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM job_effects WHERE business_key = $1", forgotten.BusinessKey); got != 0 {
		t.Fatalf("forgotten job unexpectedly produced %d effects", got)
	}

	allNormal := append(immediateKeys, "one-off-delay", "absolute-at")
	for _, key := range allNormal {
		var count, attempt int
		if err := db.QueryRow("SELECT COUNT(*), COALESCE(MAX(attempt), 0) FROM job_effects WHERE business_key = $1", key).Scan(&count, &attempt); err != nil {
			t.Fatal(err)
		}
		if count != 1 || attempt != 1 {
			t.Errorf("normal queued job %q effect count/attempt = %d/%d, want 1/1", key, count, attempt)
		}
	}
	for _, id := range []string{dispatched.Direct, dispatched.Committed, dispatched.DedupFirst, dispatched.Delayed, dispatched.AbsoluteAt} {
		if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs WHERE id = $1::uuid", id); got != 0 {
			t.Errorf("acknowledged normal job %s still has %d queue rows", id, got)
		}
	}

	for _, worker := range workers {
		stopCommandProcess(t, worker, true)
		if worker.ProcessState == nil || !worker.ProcessState.Exited() {
			t.Errorf("generated worker process was not reaped: pid=%d", worker.Process.Pid)
		}
	}
	workersRunning = false
	for index, output := range workerOutputs {
		if !strings.Contains(output.String(), "job worker_stopped") {
			t.Errorf("worker %d omitted graceful stop event:\n%s", index+1, output.String())
		}
	}

	reclaimEnvironment := jobAcceptanceEnvironment(workerEnvironment, map[string]string{
		"JOB_QUEUES":             "reclaim",
		"JOB_CONCURRENCY":        "1",
		"JOB_POLL_INTERVAL":      "25ms",
		"JOB_LEASE_DURATION":     "3s",
		"JOB_HEARTBEAT_INTERVAL": "600ms",
		"JOB_OPERATION_TIMEOUT":  "1s",
		"JOB_SHUTDOWN_TIMEOUT":   "3s",
	})
	owner := exec.Command(workerBinary)
	owner.Dir, owner.Env = runDirectory, reclaimEnvironment
	configureCommandProcess(owner)
	ownerOutput := &synchronizedBuffer{}
	owner.Stdout, owner.Stderr = ownerOutput, ownerOutput
	if err := owner.Start(); err != nil {
		t.Fatalf("start reclaim owner: %v", err)
	}
	ownerRunning := true
	t.Cleanup(func() {
		if ownerRunning {
			jobAcceptanceKillWorker(t, owner)
		}
	})
	jobAcceptanceEventually(t, 10*time.Second, func() (bool, string) {
		if !strings.Contains(ownerOutput.String(), "job worker_started") {
			return false, "reclaim owner has not reported startup"
		}
		if jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM job_attempts WHERE job_id = $1::uuid AND attempt = 1", dispatched.Reclaim) != 1 {
			return false, "reclaim owner has not entered attempt 1"
		}
		if jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs WHERE id = $1::uuid AND lease_owner IS NOT NULL", dispatched.Reclaim) != 1 {
			return false, "reclaim owner has not durably leased the job"
		}
		return true, ""
	})

	successor := exec.Command(workerBinary)
	successor.Dir, successor.Env = runDirectory, reclaimEnvironment
	configureCommandProcess(successor)
	successorOutput := &synchronizedBuffer{}
	successor.Stdout, successor.Stderr = successorOutput, successorOutput
	if err := successor.Start(); err != nil {
		t.Fatalf("start reclaim successor: %v", err)
	}
	successorRunning := true
	t.Cleanup(func() {
		if successorRunning {
			stopCommandProcess(t, successor, false)
		}
	})
	jobAcceptanceEventually(t, 10*time.Second, func() (bool, string) {
		return strings.Contains(successorOutput.String(), "job worker_started"), "reclaim successor has not reported startup"
	})
	jobAcceptanceKillWorker(t, owner)
	ownerRunning = false
	if output, err := generatedCommand(runDirectory, applicationEnvironment, helperBinary, "allow", "reclaim-active"); err != nil {
		t.Fatalf("release reclaimed handler: %v\n%s", err, output)
	}
	jobAcceptanceEventually(t, 15*time.Second, func() (bool, string) {
		if jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM job_effects WHERE business_key = $1", "reclaim-active") != 1 {
			return false, fmt.Sprintf("waiting for reclaimed effect; owner=%q successor=%q", ownerOutput.String(), successorOutput.String())
		}
		if jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs WHERE id = $1::uuid", dispatched.Reclaim) != 0 {
			return false, "waiting for successor acknowledgement"
		}
		return true, ""
	})
	var reclaimStarts, firstAttempt, lastAttempt, effectAttempt int
	if err := db.QueryRow("SELECT COUNT(*), MIN(attempt), MAX(attempt) FROM job_attempts WHERE job_id = $1::uuid", dispatched.Reclaim).Scan(&reclaimStarts, &firstAttempt, &lastAttempt); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT attempt FROM job_effects WHERE business_key = $1", "reclaim-active").Scan(&effectAttempt); err != nil {
		t.Fatal(err)
	}
	if reclaimStarts != 2 || firstAttempt != 1 || lastAttempt != 2 || effectAttempt != 2 {
		t.Fatalf("reclaim evidence starts=%d attempts=%d..%d effect_attempt=%d, want two fenced deliveries and attempt-2 effect", reclaimStarts, firstAttempt, lastAttempt, effectAttempt)
	}
	// Wait populates ProcessState on every platform. Unix reports Exited false
	// for a process terminated by a signal, even though the child was reaped.
	if owner.ProcessState == nil {
		t.Fatalf("killed reclaim owner was not reaped: pid=%d", owner.Process.Pid)
	}
	if owner.ProcessState.Success() {
		t.Fatal("killed reclaim owner unexpectedly exited successfully")
	}
	stopCommandProcess(t, successor, true)
	successorRunning = false
	if successor.ProcessState == nil || !successor.ProcessState.Exited() || !strings.Contains(successorOutput.String(), "job worker_stopped") {
		t.Fatalf("reclaim successor did not shut down cleanly:\n%s", successorOutput.String())
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs"); got != 0 {
		t.Fatalf("queue is not empty after graceful shutdown: %d active jobs", got)
	}
	if elapsed := time.Since(started); elapsed >= 4*time.Minute {
		t.Fatalf("generated durable-job workflow took %s; expected under four minutes", elapsed)
	} else {
		t.Logf("fresh generated durable-job PostgreSQL workflow completed in %s", elapsed.Round(time.Millisecond))
	}
}

type jobAcceptanceDispatches struct {
	Direct              string
	Committed           string
	RolledBack          string
	PanickedRollback    string
	PanicRecovered      bool
	DedupFirst          string
	DedupSecond         string
	DedupFirstEnqueued  bool
	DedupSecondEnqueued bool
	Delayed             string
	AbsoluteAt          string
	AbsoluteAvailable   time.Time
	Reclaim             string
	Failed              []jobAcceptanceFailedDispatch
}

type jobAcceptanceFailedDispatch struct {
	ID          string
	BusinessKey string
	Secret      string
}

func jobAcceptanceEnvironment(base []string, overrides map[string]string) []string {
	blocked := map[string]struct{}{"SESSION_SECRET": {}}
	for key := range overrides {
		blocked[strings.ToUpper(key)] = struct{}{}
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, remove := blocked[strings.ToUpper(key)]; !remove {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func jobAcceptanceCount(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("query acceptance count: %v", err)
	}
	return count
}

func jobAcceptanceEventually(t *testing.T, timeout time.Duration, condition func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := "condition not met"
	for time.Now().Before(deadline) {
		ok, detail := condition()
		if ok {
			return
		}
		if detail != "" {
			last = detail
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("eventually timed out after %s: %s", timeout, last)
}

func jobAcceptanceKillWorker(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if command.Process == nil || command.ProcessState != nil {
		return
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill active generated worker: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("killed generated worker was not reaped within 10 seconds")
	}
}

const jobAcceptanceRecordEffectSource = `package jobs

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
)

type RecordEffect struct {
	BusinessKey string
	Value       string
	Hold        bool
}

var RecordEffectDefinition = job.MustDefine[RecordEffect]("record_effect.v1", job.Policy{MaxAttempts: 3})

type RecordEffectHandler struct {
	Dependencies Dependencies
}

func (handler RecordEffectHandler) Handle(ctx context.Context, payload RecordEffect) error {
	execution, ok := job.ExecutionFromContext(ctx)
	if !ok {
		return errors.New("record effect requires job execution metadata")
	}
	if _, err := handler.Dependencies.DB.ExecContext(ctx, ` + "`" + `
		INSERT INTO job_attempts (job_id, attempt, business_key)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (job_id, attempt) DO NOTHING` + "`" + `,
		execution.ID, execution.Attempt, payload.BusinessKey); err != nil {
		return err
	}
	if payload.Hold {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			var allowSuccess bool
			err := handler.Dependencies.DB.QueryRowContext(ctx,
				"SELECT allow_success FROM job_controls WHERE business_key = $1", payload.BusinessKey).Scan(&allowSuccess)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if allowSuccess {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
	_, err := handler.Dependencies.DB.ExecContext(ctx, ` + "`" + `
		INSERT INTO job_effects (business_key, job_id, attempt, value)
		VALUES ($1, $2::uuid, $3, $4)
		ON CONFLICT (business_key) DO NOTHING` + "`" + `,
		payload.BusinessKey, execution.ID, execution.Attempt, payload.Value)
	return err
}

func DispatchRecordEffect(ctx context.Context, dispatcher job.Dispatcher, payload RecordEffect, options ...job.DispatchOption) (job.DispatchResult, error) {
	return RecordEffectDefinition.Dispatch(ctx, dispatcher, payload, options...)
}
`

const jobAcceptancePoisonMessageSource = `package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
)

type PoisonMessage struct {
	BusinessKey string
	Failure     string
	Secret      string
}

var PoisonMessageDefinition = job.MustDefine[PoisonMessage]("poison_message.v1", job.Policy{
	MaxAttempts: 2,
	Timeout:     2 * time.Second,
	Backoff:     []time.Duration{25 * time.Millisecond},
})

type PoisonMessageHandler struct {
	Dependencies Dependencies
}

func (handler PoisonMessageHandler) Handle(ctx context.Context, payload PoisonMessage) error {
	execution, ok := job.ExecutionFromContext(ctx)
	if !ok {
		return errors.New("poison message requires job execution metadata")
	}
	var allowSuccess bool
	err := handler.Dependencies.DB.QueryRowContext(ctx,
		"SELECT allow_success FROM job_controls WHERE business_key = $1", payload.BusinessKey).Scan(&allowSuccess)
	if err != nil {
		return err
	}
	if !allowSuccess {
		return fmt.Errorf("%s", payload.Failure)
	}
	_, err = handler.Dependencies.DB.ExecContext(ctx, ` + "`" + `
		INSERT INTO job_effects (business_key, job_id, attempt, value)
		VALUES ($1, $2::uuid, $3, $4)
		ON CONFLICT (business_key) DO NOTHING` + "`" + `,
		payload.BusinessKey, execution.ID, execution.Attempt, "recovered")
	return err
}

func DispatchPoisonMessage(ctx context.Context, dispatcher job.Dispatcher, payload PoisonMessage, options ...job.DispatchOption) (job.DispatchResult, error) {
	return PoisonMessageDefinition.Dispatch(ctx, dispatcher, payload, options...)
}
`

const jobAcceptanceRecordEffectTestSource = `package jobs

import (
	"context"
	"testing"
)

func TestRecordEffectHandlerRequiresExecution(t *testing.T) {
	if err := (RecordEffectHandler{}).Handle(context.Background(), RecordEffect{}); err == nil {
		t.Fatal("expected missing execution metadata error")
	}
}
`

const jobAcceptancePoisonMessageTestSource = `package jobs

import (
	"context"
	"testing"
)

func TestPoisonMessageHandlerRequiresExecution(t *testing.T) {
	if err := (PoisonMessageHandler{}).Handle(context.Background(), PoisonMessage{}); err == nil {
		t.Fatal("expected missing execution metadata error")
	}
}
`

const jobAcceptanceMigrationUp = `CREATE TABLE job_effects (
    business_key text PRIMARY KEY,
    job_id uuid NOT NULL,
    attempt integer NOT NULL CHECK (attempt > 0),
    value text NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE job_attempts (
    job_id uuid NOT NULL,
    attempt integer NOT NULL CHECK (attempt > 0),
    business_key text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (job_id, attempt)
);

CREATE TABLE job_controls (
    business_key text PRIMARY KEY,
    allow_success boolean NOT NULL DEFAULT false
);
`

const jobAcceptanceMigrationDown = `DROP TABLE IF EXISTS job_controls;
DROP TABLE IF EXISTS job_attempts;
DROP TABLE IF EXISTS job_effects;
`

const jobAcceptanceHelperSource = `package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	forgedatabase "github.com/ShanilKoshitha/goforge/database"
	"github.com/ShanilKoshitha/goforge/job"

	appdatabase "example.com/jobboard/internal/database"
	"example.com/jobboard/internal/jobs"
)

type dispatches struct {
	Direct              string
	Committed           string
	RolledBack          string
	PanickedRollback    string
	PanicRecovered      bool
	DedupFirst          string
	DedupSecond         string
	DedupFirstEnqueued  bool
	DedupSecondEnqueued bool
	Delayed             string
	AbsoluteAt          string
	AbsoluteAvailable   time.Time
	Reclaim             string
	Failed              []failedDispatch
}

type failedDispatch struct {
	ID          string
	BusinessKey string
	Secret      string
}

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	ctx := context.Background()
	db, err := appdatabase.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	if len(os.Args) == 3 && os.Args[1] == "allow" {
		_, err := db.ExecContext(ctx, "UPDATE job_controls SET allow_success = true WHERE business_key = $1", os.Args[2])
		return err
	}
	if len(os.Args) != 2 || os.Args[1] != "seed" {
		return errors.New("usage: job-acceptance <seed|allow key>")
	}
	dispatcher, err := jobs.NewDispatcher(db, nil)
	if err != nil {
		return err
	}
	result := dispatches{}
	direct, err := jobs.DispatchRecordEffect(ctx, dispatcher, jobs.RecordEffect{BusinessKey: "direct-db", Value: "direct"})
	if err != nil || !direct.Enqueued {
		return fmt.Errorf("direct dispatch: enqueued=%v err=%w", direct.Enqueued, err)
	}
	result.Direct = string(direct.ID)
	err = forgedatabase.Transaction(ctx, db, nil, func(tx *sql.Tx) error {
		committed, dispatchErr := jobs.DispatchRecordEffect(ctx, dispatcher.Using(tx), jobs.RecordEffect{BusinessKey: "transaction-commit", Value: "commit"})
		if dispatchErr == nil {
			result.Committed = string(committed.ID)
		}
		return dispatchErr
	})
	if err != nil {
		return err
	}
	rollbackMarker := errors.New("intentional rollback")
	err = forgedatabase.Transaction(ctx, db, nil, func(tx *sql.Tx) error {
		rolledBack, dispatchErr := jobs.DispatchRecordEffect(ctx, dispatcher.Using(tx), jobs.RecordEffect{BusinessKey: "transaction-rollback", Value: "rollback"})
		if dispatchErr != nil {
			return dispatchErr
		}
		result.RolledBack = string(rolledBack.ID)
		return rollbackMarker
	})
	if !errors.Is(err, rollbackMarker) {
		return fmt.Errorf("rollback probe: %w", err)
	}
	result.PanickedRollback, result.PanicRecovered = panickedRollback(ctx, db, dispatcher)
	if result.PanickedRollback == "" || !result.PanicRecovered {
		return errors.New("database.Transaction panic was not recovered after dispatch")
	}
	first, err := jobs.DispatchRecordEffect(ctx, dispatcher, jobs.RecordEffect{BusinessKey: "active-dedup", Value: "first"}, job.Deduplicate("effect:active-dedup"))
	if err != nil {
		return err
	}
	second, err := jobs.DispatchRecordEffect(ctx, dispatcher, jobs.RecordEffect{BusinessKey: "active-dedup", Value: "second"}, job.Deduplicate("effect:active-dedup"))
	if err != nil {
		return err
	}
	result.DedupFirst, result.DedupSecond = string(first.ID), string(second.ID)
	result.DedupFirstEnqueued, result.DedupSecondEnqueued = first.Enqueued, second.Enqueued
	delayed, err := jobs.DispatchRecordEffect(ctx, dispatcher, jobs.RecordEffect{BusinessKey: "one-off-delay", Value: "delayed"}, job.Delay(3*time.Second))
	if err != nil {
		return err
	}
	result.Delayed = string(delayed.ID)
	absoluteAt := time.Now().UTC().Add(5 * time.Second)
	absolute, err := jobs.DispatchRecordEffect(ctx, dispatcher, jobs.RecordEffect{BusinessKey: "absolute-at", Value: "absolute"}, job.At(absoluteAt))
	if err != nil {
		return err
	}
	result.AbsoluteAt, result.AbsoluteAvailable = string(absolute.ID), absoluteAt
	if _, err := db.ExecContext(ctx, "INSERT INTO job_controls (business_key) VALUES ($1)", "reclaim-active"); err != nil {
		return err
	}
	reclaim, err := jobs.DispatchRecordEffect(ctx, dispatcher, jobs.RecordEffect{BusinessKey: "reclaim-active", Value: "reclaimed", Hold: true}, job.Queue("reclaim"))
	if err != nil {
		return err
	}
	result.Reclaim = string(reclaim.ID)
	for index := 1; index <= 2; index++ {
		businessKey := fmt.Sprintf("poison-%d", index)
		secret := fmt.Sprintf("PAYLOAD-SECRET-%d-<script>", index)
		if _, err := db.ExecContext(ctx, "INSERT INTO job_controls (business_key) VALUES ($1)", businessKey); err != nil {
			return err
		}
		failed, err := jobs.DispatchPoisonMessage(ctx, dispatcher, jobs.PoisonMessage{
			BusinessKey: businessKey,
			Failure:     "forced failure\nsecond\tline",
			Secret:      secret,
		})
		if err != nil {
			return err
		}
		result.Failed = append(result.Failed, failedDispatch{ID: string(failed.ID), BusinessKey: businessKey, Secret: secret})
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func panickedRollback(ctx context.Context, db *sql.DB, dispatcher job.Dispatcher) (id string, recovered bool) {
	defer func() {
		if recover() != nil {
			recovered = true
		}
	}()
	_ = forgedatabase.Transaction(ctx, db, nil, func(tx *sql.Tx) error {
		dispatched, err := jobs.DispatchRecordEffect(ctx, dispatcher.Using(tx), jobs.RecordEffect{BusinessKey: "transaction-panic", Value: "panic"})
		if err != nil {
			panic(err)
		}
		id = string(dispatched.ID)
		panic("intentional transaction callback panic")
	})
	return id, false
}
`
