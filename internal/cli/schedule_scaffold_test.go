package cli

import (
	"path/filepath"
	"strings"
	"testing"

	schedulepostgres "github.com/ShanilKoshitha/goforge/schedule/postgres"
)

func TestFormatElevenScaffoldOwnsInspectableSchedulerWorkflow(t *testing.T) {
	files, err := scaffoldFiles("example.com/app", filepath.Clean("../framework"), "app")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"cmd/scheduler/main.go",
		"cmd/scheduler/main_test.go",
		"internal/schedules/registry.go",
		"database/migrations/000005_create_schedules.up.sql",
		"database/migrations/000005_create_schedules.down.sql",
	} {
		if files[path] == "" {
			t.Errorf("format-12 scaffold omits %s", path)
		}
	}
	if !strings.Contains(files["forge.yaml"], "version: 12") {
		t.Fatal("fresh scaffold must declare format 12")
	}
	if !strings.Contains(files["go.mod"], "github.com/ShanilKoshitha/goforge v0.17.0") {
		t.Fatal("fresh scaffold must pin the format-12 asset runtime")
	}
	for _, checksum := range []string{
		"github.com/robfig/cron/v3 v3.0.1 h1:WdRxkvbJztn8LMz/QEvLN5sBU+xKpSqwwUO1Pjr4qDs=",
		"github.com/robfig/cron/v3 v3.0.1/go.mod h1:eQICP3HwyT7UooqI/z+Ov+PtYAWygg1TEWWzGIFLtro=",
	} {
		if !strings.Contains(files["go.sum"], checksum) {
			t.Errorf("generated go.sum omits %q", checksum)
		}
	}
	if strings.TrimSpace(files["database/migrations/000005_create_schedules.up.sql"]) != strings.TrimSpace(schedulepostgres.Schema) {
		t.Fatal("scaffold schedule migration must match the public PostgreSQL adapter schema")
	}

	scheduler := files["cmd/scheduler/main.go"]
	for _, want := range []string{
		"config.LoadScheduler()", "jobpostgres.New(db)", "job.NewDispatcher(queueStore, db",
		"schedulepostgres.New(db)", "schedules.NewRegistry()", "schedule.NewScheduler(",
		"jobmanifest.ValidateScheduledTargets(registry)",
		"runner.RunOnce(ctx)", "runner.Run(ctx)", "signal.NotifyContext", `"event", "scheduler_started"`,
	} {
		if !strings.Contains(scheduler, want) {
			t.Errorf("generated scheduler omits %q", want)
		}
	}
	if validation, configuration := strings.Index(scheduler, "jobmanifest.ValidateScheduledTargets(registry)"), strings.Index(scheduler, "config.LoadScheduler()"); validation < 0 || configuration < 0 || validation > configuration {
		t.Fatal("generated scheduler must validate scheduled target names before configuration or database setup")
	}
	jobManifest := files["internal/jobmanifest/manifest_gen.go"]
	for _, want := range []string{"func RegisteredJobNames() []string", "func ValidateScheduledTargets(", "jobs.CleanupMailDefinition.Name()", "jobs.DeliverMailDefinition.Name()", "does not validate payload identity or deployment queue topology"} {
		if !strings.Contains(jobManifest, want) {
			t.Errorf("generated job manifest omits %q", want)
		}
	}
	if strings.Contains(files["internal/jobs/registry_gen.go"], "RegisteredJobNames") || strings.Contains(files["internal/jobs/registry_gen.go"], "ValidateScheduledTargets") {
		t.Fatal("generated job registry package must not reserve schedule-validation declarations")
	}
	registry := files["internal/schedules/registry.go"]
	if !strings.Contains(registry, "reports.daily.v1") || !strings.Contains(registry, "schedule.Register") {
		t.Fatal("schedule registry must show explicit versioned registration")
	}
	for _, want := range []string{
		"schedule.DynamicContext(func(ctx context.Context, occurrence schedule.Occurrence)",
		"if err := ctx.Err(); err != nil",
	} {
		if !strings.Contains(registry, want) {
			t.Errorf("schedule registry must demonstrate cancellation-aware payload construction with %q", want)
		}
	}
	if strings.Contains(registry, "schedule.Dynamic(") {
		t.Fatal("fresh schedule registry example must not use legacy schedule.Dynamic")
	}
	if strings.Contains(registry, "init()") || strings.Contains(registry, "reflect.") {
		t.Fatal("schedule registry must not discover definitions at runtime")
	}
	console := files["cmd/console/main.go"]
	for _, want := range []string{"schedule:list", "jobmanifest.ValidateScheduledTargets(scheduleRegistry)", "store.List(ctx, definitions)", "FINGERPRINT"} {
		if !strings.Contains(console, want) {
			t.Errorf("generated console omits schedule inspection contract %q", want)
		}
	}
	if validation, configuration := strings.Index(console, "jobmanifest.ValidateScheduledTargets(scheduleRegistry)"), strings.Index(console, "config.LoadDatabase()"); validation < 0 || configuration < 0 || validation > configuration {
		t.Fatal("schedule:list must validate scheduled target names before configuration or database setup")
	}
	config := files["internal/config/config.go"]
	for _, want := range []string{
		"func LoadScheduler()", `environment.Required("DATABASE_URL")`,
		`environment.Duration("SCHEDULE_POLL_INTERVAL", 15*time.Second)`,
		`environment.Duration("SCHEDULE_OP_TIMEOUT", 5*time.Second)`,
	} {
		if !strings.Contains(config, want) {
			t.Errorf("generated scheduler config omits %q", want)
		}
	}
	for _, environment := range []string{files[".env"], files[".env.example"]} {
		if !strings.Contains(environment, "SCHEDULE_POLL_INTERVAL=15s") || !strings.Contains(environment, "SCHEDULE_OP_TIMEOUT=5s") {
			t.Fatal("generated environment docs omit scheduler defaults")
		}
	}
}
