package schedule_test

import (
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/schedule"
)

func TestCronRejectsExtensionsAndMalformedFields(t *testing.T) {
	tests := []string{
		"* * * *", "* * * * * *", "@daily", "CRON_TZ=UTC * * * * *",
		"? * * * *", "0 0 L * *", "0 0 1W * *", "0 0 * * MON#2",
		"*/0 * * * *", "1--2 * * * *", "60 * * * *", "0 24 * * *",
		"0 0 32 * *", "0 0 * 13 *", "0 0 * * 8", "0 0 * * FUNDAY",
		"0 0 * * FRI-MON", "0 0 30 2 *",
	}
	for _, expression := range tests {
		t.Run(expression, func(t *testing.T) {
			target := jobDefinition(t, "tests.payload.v1", "default", 3)
			if _, err := schedule.Define("tests.cron.v1", expression, target, schedule.Static(testPayload{})); err == nil {
				t.Fatal("expected cron validation error")
			}
		})
	}
}

func TestCronSupportsListsRangesStepsNamesAndSundaySeven(t *testing.T) {
	definition := scheduleDefinition(t, "tests.cron.v1", "*/15 9-10 * JAN,MAR MON-FRI,7", schedule.Static(testPayload{}))
	if definition.Expression() != "*/15 9-10 * 1,3 1-5,0" {
		t.Fatalf("canonical expression = %q", definition.Expression())
	}
	start := time.Date(2026, time.January, 4, 8, 59, 0, 0, time.UTC) // Sunday.
	if next := definition.Next(start); !next.Equal(time.Date(2026, time.January, 4, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("Sunday alias did not match: %s", next)
	}

	rangeThroughSunday := scheduleDefinition(t, "tests.range.v1", "0 9 * * FRI-7/2", schedule.Static(testPayload{}))
	if rangeThroughSunday.Expression() != "0 9 * * 5,0" {
		t.Fatalf("Sunday range canonical expression = %q", rangeThroughSunday.Expression())
	}
	loneSunday := scheduleDefinition(t, "tests.sunday.v1", "0 9 * * 7", schedule.Static(testPayload{}))
	if loneSunday.Expression() != "0 9 * * 0" {
		t.Fatalf("lone Sunday canonical expression = %q", loneSunday.Expression())
	}
	fullWeekend := scheduleDefinition(t, "tests.weekend.v1", "0 9 * * 5-7", schedule.Static(testPayload{}))
	if fullWeekend.Expression() != "0 9 * * 5,6,0" {
		t.Fatalf("Friday-Sunday canonical expression = %q", fullWeekend.Expression())
	}
}

func TestCronDayOfMonthAndDayOfWeekUseOrSemantics(t *testing.T) {
	definition := scheduleDefinition(t, "tests.or.v1", "0 9 13 * FRI", schedule.Static(testPayload{}))
	next := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	var sawOnlyDay, sawOnlyWeekday bool
	for range 20 {
		next = definition.Next(next)
		day := next.Day() == 13
		weekday := next.Weekday() == time.Friday
		if !day && !weekday {
			t.Fatalf("non-matching OR occurrence: %s", next)
		}
		sawOnlyDay = sawOnlyDay || day && !weekday
		sawOnlyWeekday = sawOnlyWeekday || weekday && !day
	}
	if !sawOnlyDay || !sawOnlyWeekday {
		t.Fatalf("did not exercise both OR branches: day=%v weekday=%v", sawOnlyDay, sawOnlyWeekday)
	}
}

func TestCronNextIsMinuteExclusiveAndUTC(t *testing.T) {
	definition := scheduleDefinition(t, "tests.minute.v1", "* * * * *", schedule.Static(testPayload{}), schedule.TimeZone("America/Halifax"))
	exact := time.Date(2026, 9, 9, 12, 34, 0, 0, time.FixedZone("skew", 9*60*60))
	want := exact.UTC().Truncate(time.Minute).Add(time.Minute)
	if next := definition.Next(exact); !next.Equal(want) || next.Location() != time.UTC || next.Second() != 0 || next.Nanosecond() != 0 {
		t.Fatalf("next = %s, want %s UTC", next, want)
	}
	within := exact.Add(30*time.Second + 12*time.Nanosecond)
	if next := definition.Next(within); !next.Equal(want) {
		t.Fatalf("within-minute next = %s, want %s", next, want)
	}
}

func TestCronSkipsNonexistentSpringForwardMinute(t *testing.T) {
	definition := scheduleDefinition(t, "tests.spring.v1", "30 2 * * *", schedule.Static(testPayload{}), schedule.TimeZone("America/New_York"))
	after := time.Date(2026, time.March, 8, 6, 59, 0, 0, time.UTC) // 01:59 EST.
	want := time.Date(2026, time.March, 9, 6, 30, 0, 0, time.UTC)  // 02:30 EDT next day.
	if next := definition.Next(after); !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

func TestCronEmitsBothFallBackMinutesAsDistinctInstants(t *testing.T) {
	definition := scheduleDefinition(t, "tests.fall.v1", "30 1 * * *", schedule.Static(testPayload{}), schedule.TimeZone("America/New_York"))
	first := definition.Next(time.Date(2026, time.November, 1, 4, 0, 0, 0, time.UTC))
	second := definition.Next(first)
	wantFirst := time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC)
	wantSecond := time.Date(2026, time.November, 1, 6, 30, 0, 0, time.UTC)
	if !first.Equal(wantFirst) || !second.Equal(wantSecond) {
		t.Fatalf("fall-back occurrences = %s, %s; want %s, %s", first, second, wantFirst, wantSecond)
	}
}

func TestCronFindsLeapDayAcrossNonLeapCentury(t *testing.T) {
	definition := scheduleDefinition(t, "tests.leap.v1", "0 0 29 2 *", schedule.Static(testPayload{}))
	after := time.Date(2096, time.March, 1, 0, 0, 0, 0, time.UTC)
	want := time.Date(2104, time.February, 29, 0, 0, 0, 0, time.UTC)
	if next := definition.Next(after); !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}
