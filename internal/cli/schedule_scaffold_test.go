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
			t.Errorf("format-11 scaffold omits %s", path)
		}
	}
	if !strings.Contains(files["forge.yaml"], "version: 11") {
		t.Fatal("fresh scaffold must declare format 11")
	}
	if !strings.Contains(files["go.mod"], "github.com/ShanilKoshitha/goforge v0.14.0") {
		t.Fatal("fresh scaffold must pin the public schedule runtime")
	}
	for _, checksum := range []string{
		"github.com/ShanilKoshitha/goforge v0.14.0 h1:Dt5aHth+6iG5ODwUL8mLGL0ye6piP1klyYoGgZ3TE7k=",
		"github.com/ShanilKoshitha/goforge v0.14.0/go.mod h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=",
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
		"runner.RunOnce(ctx)", "runner.Run(ctx)", "signal.NotifyContext", `"event", "scheduler_started"`,
	} {
		if !strings.Contains(scheduler, want) {
			t.Errorf("generated scheduler omits %q", want)
		}
	}
	registry := files["internal/schedules/registry.go"]
	if !strings.Contains(registry, "reports.daily.v1") || !strings.Contains(registry, "schedule.Register") {
		t.Fatal("schedule registry must show explicit versioned registration")
	}
	if strings.Contains(registry, "init()") || strings.Contains(registry, "reflect.") {
		t.Fatal("schedule registry must not discover definitions at runtime")
	}
	console := files["cmd/console/main.go"]
	for _, want := range []string{"schedule:list", "store.List(ctx, definitions)", "FINGERPRINT"} {
		if !strings.Contains(console, want) {
			t.Errorf("generated console omits schedule inspection contract %q", want)
		}
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
