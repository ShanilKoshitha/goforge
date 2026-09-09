package schedule_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
	"github.com/ShanilKoshitha/goforge/schedule"
)

type testPayload struct {
	Value string `json:"value"`
}

func jobDefinition(t *testing.T, name, queue string, attempts int) job.Definition[testPayload] {
	t.Helper()
	definition, err := job.Define[testPayload](name, job.Policy{
		Queue: queue, MaxAttempts: attempts, Timeout: 2 * time.Second,
		Backoff: []time.Duration{time.Second, 3 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func scheduleDefinition(t *testing.T, name, expression string, factory schedule.Factory[testPayload], options ...schedule.Option) schedule.Definition {
	t.Helper()
	definition, err := schedule.Define(name, expression, jobDefinition(t, "tests.payload.v1", "reports", 4), factory, options...)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func TestDefinitionAppliesInspectableDefaults(t *testing.T) {
	definition := scheduleDefinition(t, "reports.daily.v1", "  0  6 * * *  ", schedule.Static(testPayload{Value: "daily"}))
	if definition.Name() != "reports.daily.v1" || definition.Expression() != "0 6 * * *" || definition.TimeZone() != "UTC" {
		t.Fatalf("unexpected metadata: %s %s %s", definition.Name(), definition.Expression(), definition.TimeZone())
	}
	if definition.MisfireGrace() != schedule.DefaultMisfireGrace || definition.JobName() != "tests.payload.v1" || definition.Queue() != "reports" {
		t.Fatalf("unexpected binding metadata: grace=%s job=%s queue=%s", definition.MisfireGrace(), definition.JobName(), definition.Queue())
	}
	if len(definition.Fingerprint()) != 64 {
		t.Fatalf("fingerprint = %q", definition.Fingerprint())
	}
	policy := definition.JobPolicy()
	policy.Backoff[0] = time.Hour
	if definition.JobPolicy().Backoff[0] != time.Second {
		t.Fatal("job policy accessor leaked mutable backoff state")
	}
	if next := definition.Next(time.Date(2026, 9, 9, 6, 0, 0, 0, time.UTC)); !next.Equal(time.Date(2026, 9, 10, 6, 0, 0, 0, time.UTC)) || next.Location() != time.UTC {
		t.Fatalf("next = %s", next)
	}
}

func TestDefinitionRejectsInvalidContracts(t *testing.T) {
	target := jobDefinition(t, "tests.payload.v1", "default", 3)
	validFactory := schedule.Static(testPayload{})
	tests := []struct {
		name       string
		schedule   string
		expression string
		target     job.Definition[testPayload]
		factory    schedule.Factory[testPayload]
		options    []schedule.Option
	}{
		{name: "unversioned", schedule: "reports.daily", expression: "* * * * *", target: target, factory: validFactory},
		{name: "zero version", schedule: "reports.daily.v0", expression: "* * * * *", target: target, factory: validFactory},
		{name: "uppercase", schedule: "Reports.daily.v1", expression: "* * * * *", target: target, factory: validFactory},
		{name: "surrounding space", schedule: " reports.daily.v1", expression: "* * * * *", target: target, factory: validFactory},
		{name: "too long", schedule: "a" + strings.Repeat("b", schedule.MaxNameBytes) + ".v1", expression: "* * * * *", target: target, factory: validFactory},
		{name: "zero job", schedule: "reports.daily.v1", expression: "* * * * *", factory: validFactory},
		{name: "zero factory", schedule: "reports.daily.v1", expression: "* * * * *", target: target},
		{name: "nil dynamic", schedule: "reports.daily.v1", expression: "* * * * *", target: target, factory: schedule.Dynamic[testPayload](nil)},
		{name: "nil option", schedule: "reports.daily.v1", expression: "* * * * *", target: target, factory: validFactory, options: []schedule.Option{nil}},
		{name: "local zone", schedule: "reports.daily.v1", expression: "* * * * *", target: target, factory: validFactory, options: []schedule.Option{schedule.TimeZone("Local")}},
		{name: "fixed zone", schedule: "reports.daily.v1", expression: "* * * * *", target: target, factory: validFactory, options: []schedule.Option{schedule.TimeZone("EST")}},
		{name: "unknown zone", schedule: "reports.daily.v1", expression: "* * * * *", target: target, factory: validFactory, options: []schedule.Option{schedule.TimeZone("Mars/Olympus")}},
		{name: "path zone", schedule: "reports.daily.v1", expression: "* * * * *", target: target, factory: validFactory, options: []schedule.Option{schedule.TimeZone("America/../UTC")}},
		{name: "short grace", schedule: "reports.daily.v1", expression: "* * * * *", target: target, factory: validFactory, options: []schedule.Option{schedule.MisfireGrace(time.Second)}},
		{name: "fractional grace", schedule: "reports.daily.v1", expression: "* * * * *", target: target, factory: validFactory, options: []schedule.Option{schedule.MisfireGrace(90 * time.Second)}},
		{name: "long grace", schedule: "reports.daily.v1", expression: "* * * * *", target: target, factory: validFactory, options: []schedule.Option{schedule.MisfireGrace(366 * 24 * time.Hour)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := schedule.Define(test.schedule, test.expression, test.target, test.factory, test.options...); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestStaticRejectsUnsupportedPayload(t *testing.T) {
	target := job.MustDefine[chan int]("tests.channel.v1", job.Policy{})
	if _, err := schedule.Define("tests.channel.v1", "* * * * *", target, schedule.Static(make(chan int))); err == nil {
		t.Fatal("expected static JSON error")
	}
}

func TestFingerprintCoversEveryBehavioralInput(t *testing.T) {
	baseTarget := jobDefinition(t, "tests.payload.v1", "reports", 4)
	base := func() schedule.Definition {
		definition, err := schedule.Define("reports.daily.v1", "0 6 * * *", baseTarget, schedule.Static(testPayload{Value: "one"}))
		if err != nil {
			t.Fatal(err)
		}
		return definition
	}
	first := base()
	if first.Fingerprint() != base().Fingerprint() {
		t.Fatal("identical definitions produced different fingerprints")
	}
	cases := map[string]schedule.Definition{
		"name":   schedule.MustDefine("reports.daily.v2", "0 6 * * *", baseTarget, schedule.Static(testPayload{Value: "one"})),
		"cron":   schedule.MustDefine("reports.daily.v1", "1 6 * * *", baseTarget, schedule.Static(testPayload{Value: "one"})),
		"zone":   schedule.MustDefine("reports.daily.v1", "0 6 * * *", baseTarget, schedule.Static(testPayload{Value: "one"}), schedule.TimeZone("America/Halifax")),
		"grace":  schedule.MustDefine("reports.daily.v1", "0 6 * * *", baseTarget, schedule.Static(testPayload{Value: "one"}), schedule.MisfireGrace(16*time.Minute)),
		"job":    schedule.MustDefine("reports.daily.v1", "0 6 * * *", jobDefinition(t, "tests.payload.v2", "reports", 4), schedule.Static(testPayload{Value: "one"})),
		"queue":  schedule.MustDefine("reports.daily.v1", "0 6 * * *", jobDefinition(t, "tests.payload.v1", "other", 4), schedule.Static(testPayload{Value: "one"})),
		"policy": schedule.MustDefine("reports.daily.v1", "0 6 * * *", jobDefinition(t, "tests.payload.v1", "reports", 5), schedule.Static(testPayload{Value: "one"})),
		"timeout": schedule.MustDefine("reports.daily.v1", "0 6 * * *", job.MustDefine[testPayload]("tests.payload.v1", job.Policy{
			Queue: "reports", MaxAttempts: 4, Timeout: 3 * time.Second, Backoff: []time.Duration{time.Second, 3 * time.Second},
		}), schedule.Static(testPayload{Value: "one"})),
		"backoff": schedule.MustDefine("reports.daily.v1", "0 6 * * *", job.MustDefine[testPayload]("tests.payload.v1", job.Policy{
			Queue: "reports", MaxAttempts: 4, Timeout: 2 * time.Second, Backoff: []time.Duration{time.Second, 4 * time.Second},
		}), schedule.Static(testPayload{Value: "one"})),
		"payload":  schedule.MustDefine("reports.daily.v1", "0 6 * * *", baseTarget, schedule.Static(testPayload{Value: "two"})),
		"strategy": schedule.MustDefine("reports.daily.v1", "0 6 * * *", baseTarget, schedule.Dynamic(func(schedule.Occurrence) (testPayload, error) { return testPayload{Value: "one"}, nil })),
	}
	for name, changed := range cases {
		if first.Fingerprint() == changed.Fingerprint() {
			t.Errorf("%s did not change fingerprint", name)
		}
	}
}

func TestDynamicFingerprintUsesVersionedStrategyMarkerNotFunctionIdentity(t *testing.T) {
	target := jobDefinition(t, "tests.payload.v1", "default", 3)
	first := schedule.MustDefine("reports.dynamic.v1", "* * * * *", target, schedule.Dynamic(func(schedule.Occurrence) (testPayload, error) {
		return testPayload{Value: "first"}, nil
	}))
	second := schedule.MustDefine("reports.dynamic.v1", "* * * * *", target, schedule.Dynamic(func(schedule.Occurrence) (testPayload, error) {
		return testPayload{Value: "second"}, nil
	}))
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatal("dynamic function identity leaked into fingerprint")
	}
}

func TestRegistryIsExplicitSortedAndRejectsDuplicates(t *testing.T) {
	registry := schedule.NewRegistry()
	second := scheduleDefinition(t, "reports.second.v1", "* * * * *", schedule.Static(testPayload{}))
	first := scheduleDefinition(t, "reports.first.v1", "* * * * *", schedule.Static(testPayload{}))
	if err := schedule.Register(registry, second); err != nil {
		t.Fatal(err)
	}
	if err := schedule.Register(registry, first); err != nil {
		t.Fatal(err)
	}
	if err := schedule.Register(registry, first); err == nil {
		t.Fatal("expected duplicate error")
	}
	names := registry.Names()
	if len(names) != 2 || names[0] != "reports.first.v1" || names[1] != "reports.second.v1" {
		t.Fatalf("names = %v", names)
	}
	if err := schedule.Register(nil, first); err == nil {
		t.Fatal("nil registry was accepted")
	}
	if err := schedule.Register(registry, schedule.Definition{}); err == nil {
		t.Fatal("zero definition was accepted")
	}
}

func TestDynamicFactoryErrorRetainsCause(t *testing.T) {
	sentinel := errors.New("payload unavailable")
	definition := scheduleDefinition(t, "reports.error.v1", "* * * * *", schedule.Dynamic(func(schedule.Occurrence) (testPayload, error) {
		return testPayload{}, sentinel
	}))
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, definition)
	jobStore := &recordingJobStore{}
	dispatcher, err := job.NewDispatcher(jobStore, inertExecutor{}, job.DispatcherConfig{})
	if err != nil {
		t.Fatal(err)
	}
	store := &callbackScheduleStore{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)}
	scheduler, err := schedule.NewScheduler(store, registry, dispatcher, schedule.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = scheduler.RunOnce(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}
