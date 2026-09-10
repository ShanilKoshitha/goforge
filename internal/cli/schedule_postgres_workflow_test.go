package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestGeneratedSchedulePostgresWorkflow(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}

	root := projectRoot(t)
	scratch := t.TempDir()
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	forgeBinary := filepath.Join(scratch, "forge"+extension)
	schedulerBinary := filepath.Join(scratch, "schedule-runner"+extension)
	workerBinary := filepath.Join(scratch, "schedule-worker"+extension)
	baseEnvironment := append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := generatedCommand(root, baseEnvironment, "go", "build", "-o", forgeBinary, "./cmd/forge"); err != nil {
		t.Fatalf("build forge CLI: %v\n%s", err, output)
	}

	directory := filepath.Join(scratch, "schedule-app")
	if output, err := generatedCommand(scratch, baseEnvironment, forgeBinary, "new", "schedule-app", "--module", "example.com/scheduleapp", "--replace", root); err != nil {
		t.Fatalf("forge new: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, baseEnvironment, forgeBinary, "make:job", "RecordScheduledEffect"); err != nil {
		t.Fatalf("forge make:job: %v\n%s", err, output)
	}

	fixtures := map[string]string{
		filepath.Join("internal", "jobs", "record_scheduled_effect.go"):            scheduleAcceptanceJobSource,
		filepath.Join("internal", "jobs", "record_scheduled_effect_test.go"):       scheduleAcceptanceJobTestSource,
		filepath.Join("internal", "schedules", "registry.go"):                      scheduleAcceptanceRegistrySource,
		filepath.Join("database", "migrations", "900001_schedule_effect.up.sql"):   scheduleAcceptanceMigrationUp,
		filepath.Join("database", "migrations", "900001_schedule_effect.down.sql"): scheduleAcceptanceMigrationDown,
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
		filepath.Join("internal", "jobs", "record_scheduled_effect.go"),
		filepath.Join("internal", "jobs", "record_scheduled_effect_test.go"),
		filepath.Join("internal", "schedules", "registry.go")); err != nil {
		t.Fatalf("gofmt schedule fixtures: %v\n%s", err, output)
	}

	adminDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	adminContext, adminCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := adminDB.PingContext(adminContext); err != nil {
		adminCancel()
		_ = adminDB.Close()
		t.Fatalf("connect to acceptance PostgreSQL: %v", err)
	}
	adminCancel()
	schema := fmt.Sprintf("goforge_schedule_acceptance_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create isolated PostgreSQL schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, dropErr := adminDB.ExecContext(cleanupContext, "DROP SCHEMA "+schema+" CASCADE"); dropErr != nil {
			t.Errorf("drop isolated PostgreSQL schema: %v", dropErr)
		}
		_ = adminDB.Close()
	})
	isolationURL, err := postgresSchemaURL(databaseURL, schema)
	if err != nil {
		t.Fatal(err)
	}
	applicationEnvironment := jobAcceptanceEnvironment(baseEnvironment, map[string]string{
		"APP_ENV":                  "local",
		"DATABASE_URL":             isolationURL,
		"JOB_QUEUES":               "default",
		"JOB_CONCURRENCY":          "1",
		"JOB_POLL_INTERVAL":        "25ms",
		"JOB_LEASE_DURATION":       "5s",
		"JOB_HEARTBEAT_INTERVAL":   "1s",
		"JOB_OPERATION_TIMEOUT":    "1s",
		"JOB_CANCELLATION_TIMEOUT": "2s",
		"JOB_SHUTDOWN_TIMEOUT":     "3s",
		"MAIL_SMTP_ADDRESS":        "127.0.0.1:1",
		"MAIL_SMTP_TLS":            "none",
		"MAIL_SMTP_SERVER_NAME":    "localhost",
		"MAIL_SMTP_TIMEOUT":        "1s",
		"MAIL_OUTBOX_KEY":          base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x73}, 32)),
		"SCHEDULE_POLL_INTERVAL":   "1s",
		"SCHEDULE_OP_TIMEOUT":      "60s",
	})
	if output, err := generatedCommand(directory, applicationEnvironment, forgeBinary, "migrate"); err != nil {
		t.Fatalf("migrate generated schedule application: %v\n%s", err, output)
	} else if !strings.Contains(output, "000005_create_schedules") || !strings.Contains(output, "900001_schedule_effect") {
		t.Fatalf("schedule migrations were not applied:\n%s", output)
	}
	var definitionFingerprint string
	if output, err := generatedCommand(directory, applicationEnvironment, forgeBinary, "schedule:list"); err != nil {
		t.Fatalf("list code-owned schedule before materialization: %v\n%s", err, output)
	} else {
		definitionFingerprint = scheduleAcceptanceFingerprint(t, output)
	}
	for _, gate := range [][]string{{"test", "./..."}, {"test", "-race", "./..."}, {"vet", "./..."}} {
		if output, err := generatedCommand(directory, applicationEnvironment, "go", gate...); err != nil {
			t.Fatalf("fresh schedule application go %s: %v\n%s", strings.Join(gate, " "), err, output)
		}
	}
	if output, err := generatedCommand(directory, applicationEnvironment, "go", "build", "-o", schedulerBinary, "./cmd/scheduler"); err != nil {
		t.Fatalf("build generated scheduler: %v\n%s", err, output)
	}
	if output, err := generatedCommand(directory, applicationEnvironment, "go", "build", "-o", workerBinary, "./cmd/worker"); err != nil {
		t.Fatalf("build generated worker: %v\n%s", err, output)
	}
	runDirectory := filepath.Join(scratch, "outside-source-cwd")
	if err := os.MkdirAll(runDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	invalidEnvironment := jobAcceptanceEnvironment(applicationEnvironment, map[string]string{
		"DATABASE_URL":                  "postgres://invalid:invalid@127.0.0.1:1/invalid?sslmode=disable",
		"GOFORGE_TEST_INVALID_SCHEDULE": "1",
	})
	if output, err := generatedCommand(runDirectory, invalidEnvironment, schedulerBinary, "--once"); err == nil {
		t.Fatalf("scheduler accepted a target name absent from the generated job registry:\n%s", output)
	} else if !strings.Contains(output, "acceptance.missing-handler.v1 -> acceptance.unregistered-job.v1") || strings.Contains(output, "connect") {
		t.Fatalf("scheduler did not reject the missing handler before database setup:\n%s", output)
	}
	if output, err := generatedCommand(directory, invalidEnvironment, forgeBinary, "schedule:list"); err == nil {
		t.Fatalf("schedule:list accepted a target name absent from the generated job registry:\n%s", output)
	} else if !strings.Contains(output, "acceptance.missing-handler.v1 -> acceptance.unregistered-job.v1") || strings.Contains(output, "connect") {
		t.Fatalf("schedule:list did not share pre-database target validation:\n%s", output)
	}

	db, err := sql.Open("pgx", isolationURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	seedMinute := scheduleAcceptanceDatabaseMinute(t, db)
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO goforge_schedules
			(name, fingerprint, cron_expression, time_zone, job_name, queue,
			 misfire_grace_ms, overlap_policy, next_run_at, created_at, updated_at)
		VALUES ($1, $2, '* * * * *', 'UTC', $3, 'default', 900000, 'forbid', $4, clock_timestamp(), clock_timestamp())`,
		scheduleAcceptanceName, definitionFingerprint, scheduleAcceptanceJobName, seedMinute,
	); err != nil {
		t.Fatalf("seed due schedule cursor: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		CREATE FUNCTION block_schedule_acceptance_enqueue() RETURNS trigger AS $$
		BEGIN
			IF NEW.dedup_key = 'goforge:schedule:acceptance.every-minute.v1' THEN
				PERFORM pg_advisory_xact_lock(hashtext(NEW.dedup_key));
			END IF;
			RETURN NEW;
		END
		$$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("install scheduler overlap barrier function: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `CREATE TRIGGER block_schedule_acceptance_enqueue
		BEFORE INSERT ON goforge_jobs
		FOR EACH ROW EXECUTE FUNCTION block_schedule_acceptance_enqueue()`); err != nil {
		t.Fatalf("install scheduler overlap barrier trigger: %v", err)
	}
	barrier, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin scheduler overlap barrier: %v", err)
	}
	barrierOpen := true
	t.Cleanup(func() {
		if barrierOpen {
			_ = barrier.Rollback()
		}
	})
	if _, err := barrier.ExecContext(context.Background(), "SELECT pg_advisory_xact_lock(hashtext($1))", "goforge:schedule:"+scheduleAcceptanceName); err != nil {
		t.Fatalf("hold scheduler overlap barrier: %v", err)
	}

	type processResult struct {
		output string
		err    error
	}
	startScheduler := func(name string, environment []string) (*exec.Cmd, <-chan processResult) {
		t.Helper()
		command := exec.Command(schedulerBinary, "--once")
		command.Dir = runDirectory
		command.Env = environment
		configureCommandProcess(command)
		output := &bytes.Buffer{}
		command.Stdout, command.Stderr = output, output
		if err := command.Start(); err != nil {
			t.Fatalf("start %s generated scheduler: %v", name, err)
		}
		t.Cleanup(func() {
			if command.Process != nil {
				_ = command.Process.Kill()
			}
		})
		result := make(chan processResult, 1)
		go func() {
			err := command.Wait()
			result <- processResult{output: output.String(), err: err}
		}()
		return command, result
	}
	waitScheduler := func(name string, command *exec.Cmd, result <-chan processResult) processResult {
		t.Helper()
		select {
		case completed := <-result:
			return completed
		case <-time.After(15 * time.Second):
			_ = command.Process.Kill()
			completed := <-result
			t.Fatalf("%s generated scheduler did not exit within 15s\n%s", name, completed.output)
			return processResult{}
		}
	}
	contenderAEnvironment := jobAcceptanceEnvironment(applicationEnvironment, map[string]string{
		"DATABASE_URL": scheduleAcceptanceApplicationURL(t, isolationURL, "goforge_schedule_contender_a"),
	})
	contenderA, contenderAResult := startScheduler("first competing", contenderAEnvironment)
	jobAcceptanceEventually(t, 10*time.Second, func() (bool, string) {
		waiting := jobAcceptanceCount(t, db, `
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE application_name = 'goforge_schedule_contender_a'
			  AND wait_event_type = 'Lock' AND lower(wait_event) = 'advisory'`)
		return waiting == 1, fmt.Sprintf("first scheduler advisory waiters=%d", waiting)
	})
	contenderB, contenderBResult := startScheduler("second competing", applicationEnvironment)
	second := waitScheduler("second competing", contenderB, contenderBResult)
	if second.err != nil || !strings.Contains(second.output, scheduleAcceptanceName+"\tcontended\t-\t-") {
		t.Fatalf("second scheduler did not report durable contention: %v\n%s", second.err, second.output)
	}
	if err := barrier.Commit(); err != nil {
		t.Fatalf("release scheduler overlap barrier: %v", err)
	}
	barrierOpen = false
	first := waitScheduler("first competing", contenderA, contenderAResult)
	if first.err != nil || !strings.Contains(first.output, scheduleAcceptanceName+"\tenqueued\t") {
		t.Fatalf("first scheduler did not enqueue after overlap barrier release: %v\n%s", first.err, first.output)
	}
	combinedOutput := first.output + second.output
	if enqueued, contended := strings.Count(combinedOutput, "\tenqueued\t"), strings.Count(combinedOutput, "\tcontended\t"); enqueued != 1 || contended != 1 {
		t.Fatalf("competing outcomes enqueued=%d contended=%d, want exactly one each\n%s", enqueued, contended, combinedOutput)
	}

	var occurrenceAt, evaluatedAt, nextRunAt time.Time
	var lastJobID, lastOutcome string
	if err := db.QueryRowContext(context.Background(), `
		SELECT last_occurrence_at, last_evaluated_at, next_run_at, last_job_id, last_outcome
		FROM goforge_schedules WHERE name = $1`, scheduleAcceptanceName).Scan(
		&occurrenceAt, &evaluatedAt, &nextRunAt, &lastJobID, &lastOutcome,
	); err != nil {
		t.Fatalf("read durable schedule cursor: %v\nprocess output:\n%s", err, combinedOutput)
	}
	occurrenceAt = occurrenceAt.UTC()
	evaluatedAt = evaluatedAt.UTC()
	nextRunAt = nextRunAt.UTC()
	if !occurrenceAt.Equal(evaluatedAt.Truncate(time.Minute)) {
		t.Fatalf("schedule occurrence does not match its evaluated database minute: seed=%s occurrence=%s evaluated=%s\n%s",
			seedMinute.Format(time.RFC3339), occurrenceAt.Format(time.RFC3339), evaluatedAt.Format(time.RFC3339Nano), combinedOutput)
	}
	if !nextRunAt.Equal(occurrenceAt.Add(time.Minute)) || lastOutcome != "enqueued" || lastJobID == "" {
		t.Fatalf("durable schedule state next=%s outcome=%q job=%q, want next minute and one enqueue\n%s",
			nextRunAt.Format(time.RFC3339), lastOutcome, lastJobID, combinedOutput)
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_schedules WHERE name = $1", scheduleAcceptanceName); got != 1 {
		t.Fatalf("competing schedulers created %d durable cursor rows, want 1", got)
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs WHERE name = $1 AND dedup_key = $2",
		scheduleAcceptanceJobName, "goforge:schedule:"+scheduleAcceptanceName); got != 1 {
		t.Fatalf("competing schedulers created %d active jobs for one occurrence, want 1\n%s", got, combinedOutput)
	}
	var queuedID, businessKey, payloadTime string
	if err := db.QueryRowContext(context.Background(), `
		SELECT id::text, payload->>'business_key', payload->>'scheduled_at'
		FROM goforge_jobs WHERE name = $1 AND dedup_key = $2`,
		scheduleAcceptanceJobName, "goforge:schedule:"+scheduleAcceptanceName,
	).Scan(&queuedID, &businessKey, &payloadTime); err != nil {
		t.Fatalf("read scheduled queue payload: %v", err)
	}
	parsedPayloadTime, err := time.Parse(time.RFC3339Nano, payloadTime)
	if err != nil {
		t.Fatalf("parse scheduled payload occurrence %q: %v", payloadTime, err)
	}
	if queuedID != lastJobID || businessKey != scheduleAcceptanceBusinessKey || !parsedPayloadTime.Equal(occurrenceAt) {
		t.Fatalf("scheduled payload id=%q key=%q at=%s, cursor id=%q minute=%s",
			queuedID, businessKey, parsedPayloadTime.Format(time.RFC3339Nano), lastJobID, occurrenceAt.Format(time.RFC3339))
	}
	if got := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM schedule_effects"); got != 0 {
		t.Fatalf("scheduler executed %d job effects directly; only a worker may run handlers", got)
	}
	if output, err := generatedCommand(directory, applicationEnvironment, forgeBinary, "schedule:list"); err != nil {
		t.Fatalf("forge schedule:list: %v\n%s", err, output)
	} else if !strings.Contains(output, scheduleAcceptanceName) || !strings.Contains(output, queuedID) || !strings.Contains(output, "enqueued") {
		t.Fatalf("schedule:list omitted durable state:\n%s", output)
	}

	workerOutput := &synchronizedBuffer{}
	worker := exec.Command(workerBinary)
	worker.Dir = runDirectory
	worker.Env = applicationEnvironment
	worker.Stdout, worker.Stderr = workerOutput, workerOutput
	configureCommandProcess(worker)
	if err := worker.Start(); err != nil {
		t.Fatalf("start generated worker outside source directory: %v", err)
	}
	workerRunning := true
	t.Cleanup(func() {
		if workerRunning {
			stopCommandProcess(t, worker, false)
		}
	})
	jobAcceptanceEventually(t, 12*time.Second, func() (bool, string) {
		effects := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM schedule_effects WHERE business_key = $1", scheduleAcceptanceBusinessKey)
		queued := jobAcceptanceCount(t, db, "SELECT COUNT(*) FROM goforge_jobs WHERE id = $1::uuid", queuedID)
		return effects == 1 && queued == 0, fmt.Sprintf("effect=%d queued=%d worker=%s", effects, queued, workerOutput.String())
	})
	var effectJobID string
	var effectAttempt int
	var effectScheduledAt time.Time
	if err := db.QueryRowContext(context.Background(), `
		SELECT job_id::text, attempt, scheduled_at
		FROM schedule_effects WHERE business_key = $1`, scheduleAcceptanceBusinessKey,
	).Scan(&effectJobID, &effectAttempt, &effectScheduledAt); err != nil {
		t.Fatalf("read application-owned scheduled effect: %v", err)
	}
	if effectJobID != queuedID || effectAttempt != 1 || !effectScheduledAt.UTC().Equal(occurrenceAt) {
		t.Fatalf("worker effect job=%q attempt=%d at=%s, want job=%q attempt=1 at=%s",
			effectJobID, effectAttempt, effectScheduledAt.UTC().Format(time.RFC3339), queuedID, occurrenceAt.Format(time.RFC3339))
	}
	stopCommandProcess(t, worker, true)
	workerRunning = false

	daemonOutput := &synchronizedBuffer{}
	daemon := exec.Command(schedulerBinary)
	daemon.Dir = runDirectory
	daemon.Env = applicationEnvironment
	daemon.Stdout, daemon.Stderr = daemonOutput, daemonOutput
	configureCommandProcess(daemon)
	if err := daemon.Start(); err != nil {
		t.Fatalf("start generated scheduler daemon outside source directory: %v", err)
	}
	daemonRunning := true
	t.Cleanup(func() {
		if daemonRunning {
			stopCommandProcess(t, daemon, false)
		}
	})
	jobAcceptanceEventually(t, 5*time.Second, func() (bool, string) {
		output := daemonOutput.String()
		return strings.Contains(output, "event=scheduler_started") && strings.Contains(output, "mode=work"), output
	})
	stopCommandProcess(t, daemon, true)
	daemonRunning = false
	if daemon.ProcessState == nil || !daemon.ProcessState.Exited() {
		t.Fatalf("generated scheduler daemon was not reaped: pid=%d", daemon.Process.Pid)
	}
}

func scheduleAcceptanceDatabaseMinute(t *testing.T, db *sql.DB) time.Time {
	t.Helper()
	queryContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var now time.Time
	if err := db.QueryRowContext(queryContext, "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatalf("read PostgreSQL clock: %v", err)
	}
	return now.UTC().Truncate(time.Minute)
}

func scheduleAcceptanceFingerprint(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, scheduleAcceptanceName) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 0 {
			fingerprint := fields[len(fields)-1]
			if len(fingerprint) == 64 {
				return fingerprint
			}
		}
	}
	t.Fatalf("schedule:list omitted a valid fingerprint for %s:\n%s", scheduleAcceptanceName, output)
	return ""
}

func scheduleAcceptanceApplicationURL(t *testing.T, databaseURL, applicationName string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse schedule acceptance database URL: %v", err)
	}
	query := parsed.Query()
	query.Set("application_name", applicationName)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

const (
	scheduleAcceptanceName        = "acceptance.every-minute.v1"
	scheduleAcceptanceJobName     = "record_scheduled_effect.v1"
	scheduleAcceptanceBusinessKey = "generated-scheduler-current-minute"
)

const scheduleAcceptanceJobSource = `package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
)

type RecordScheduledEffect struct {
	BusinessKey string    ` + "`json:\"business_key\"`" + `
	ScheduledAt time.Time ` + "`json:\"scheduled_at\"`" + `
}

var RecordScheduledEffectDefinition = job.MustDefine[RecordScheduledEffect](
	"record_scheduled_effect.v1",
	job.Policy{MaxAttempts: 3, Timeout: 5 * time.Second},
)

type RecordScheduledEffectHandler struct {
	Dependencies Dependencies
}

func (handler RecordScheduledEffectHandler) Handle(ctx context.Context, payload RecordScheduledEffect) error {
	if handler.Dependencies.DB == nil {
		return errors.New("record scheduled effect requires a database")
	}
	execution, ok := job.ExecutionFromContext(ctx)
	if !ok {
		return errors.New("record scheduled effect requires job execution metadata")
	}
	_, err := handler.Dependencies.DB.ExecContext(ctx,
		"INSERT INTO schedule_effects (business_key, scheduled_at, job_id, attempt) VALUES ($1, $2, $3::uuid, $4) ON CONFLICT (business_key) DO NOTHING",
		payload.BusinessKey, payload.ScheduledAt, execution.ID, execution.Attempt,
	)
	return err
}

func DispatchRecordScheduledEffect(ctx context.Context, dispatcher job.Dispatcher, payload RecordScheduledEffect, options ...job.DispatchOption) (job.DispatchResult, error) {
	return RecordScheduledEffectDefinition.Dispatch(ctx, dispatcher, payload, options...)
}
`

const scheduleAcceptanceJobTestSource = `package jobs

import (
	"context"
	"testing"
)

func TestRecordScheduledEffectHandlerRequiresDependencies(t *testing.T) {
	if err := (RecordScheduledEffectHandler{}).Handle(context.Background(), RecordScheduledEffect{}); err == nil {
		t.Fatal("handler accepted missing application dependencies")
	}
}
`

const scheduleAcceptanceRegistrySource = `package schedules

import (
	"fmt"
	"os"

	"github.com/ShanilKoshitha/goforge/job"
	"github.com/ShanilKoshitha/goforge/schedule"

	"example.com/scheduleapp/internal/jobs"
)

var everyMinute = schedule.MustDefine(
	"acceptance.every-minute.v1",
	"* * * * *",
	jobs.RecordScheduledEffectDefinition,
	schedule.Dynamic(func(occurrence schedule.Occurrence) (jobs.RecordScheduledEffect, error) {
		return jobs.RecordScheduledEffect{
			BusinessKey: "generated-scheduler-current-minute",
			ScheduledAt: occurrence.ScheduledAt,
		}, nil
	}),
)

var missingHandler = schedule.MustDefine(
	"acceptance.missing-handler.v1",
	"* * * * *",
	job.MustDefine[struct{}]("acceptance.unregistered-job.v1", job.Policy{}),
	schedule.Static(struct{}{}),
)

func NewRegistry() (*schedule.Registry, error) {
	registry := schedule.NewRegistry()
	if err := schedule.Register(registry, everyMinute); err != nil {
		return nil, fmt.Errorf("register application schedules: %w", err)
	}
	if os.Getenv("GOFORGE_TEST_INVALID_SCHEDULE") == "1" {
		if err := schedule.Register(registry, missingHandler); err != nil {
			return nil, fmt.Errorf("register invalid acceptance schedule: %w", err)
		}
	}
	return registry, nil
}
`

const scheduleAcceptanceMigrationUp = `CREATE TABLE schedule_effects (
    business_key text PRIMARY KEY,
    scheduled_at timestamptz NOT NULL,
    job_id uuid NOT NULL,
    attempt integer NOT NULL CHECK (attempt > 0),
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
`

const scheduleAcceptanceMigrationDown = `DROP TABLE IF EXISTS schedule_effects;
`
