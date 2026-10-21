package config_test

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/config"
)

func TestReaderFieldErrorsAreTypedAndDoNotRetainSuppliedValues(t *testing.T) {
	const secret = "smtp-password-and-session-secret-7dcb"
	for _, test := range []struct {
		name, key, rule, value, message string
		read                            func(*config.Reader) any
		fallback                        any
	}{
		{"required", "SESSION_SECRET", "required", " \t\r\n", "SESSION_SECRET is required", func(reader *config.Reader) any { return reader.Required("SESSION_SECRET") }, ""},
		{"integer", "JOB_CONCURRENCY", "integer", secret, "JOB_CONCURRENCY must be an integer", func(reader *config.Reader) any { return reader.Int("JOB_CONCURRENCY", 4) }, 4},
		{"integer overflow", "JOB_CONCURRENCY", "integer", strings.Repeat("9", 100), "JOB_CONCURRENCY must be an integer", func(reader *config.Reader) any { return reader.Int("JOB_CONCURRENCY", 4) }, 4},
		{"boolean", "APP_ENABLE_HSTS", "boolean", secret, "APP_ENABLE_HSTS must be a boolean", func(reader *config.Reader) any { return reader.Bool("APP_ENABLE_HSTS", true) }, true},
		{"duration", "MAIL_SMTP_TIMEOUT", "duration", secret, "MAIL_SMTP_TIMEOUT must be a duration", func(reader *config.Reader) any { return reader.Duration("MAIL_SMTP_TIMEOUT", time.Second) }, time.Second},
		{"duration overflow", "MAIL_SMTP_TIMEOUT", "duration", strings.Repeat("9", 100) + "h", "MAIL_SMTP_TIMEOUT must be a duration", func(reader *config.Reader) any { return reader.Duration("MAIL_SMTP_TIMEOUT", time.Second) }, time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := config.FromMap(map[string]string{test.key: test.value})
			if got := test.read(reader); got != test.fallback {
				t.Fatalf("fallback = %v, want %v", got, test.fallback)
			}
			joined := reader.Err()
			if joined == nil || joined.Error() != test.message {
				t.Fatalf("error = %v, want %q", joined, test.message)
			}
			wrapped := fmt.Errorf("load configuration: %w", joined)
			var field *config.FieldError
			if !errors.As(wrapped, &field) || field.Key != test.key || field.Rule != test.rule {
				t.Fatalf("typed field error = %#v", field)
			}
			walkConfigErrors(wrapped, func(err error) {
				if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), test.value) {
					t.Errorf("error chain retained supplied value: %T", err)
				}
				if _, ok := err.(*strconv.NumError); ok {
					t.Errorf("error chain retained a value-bearing conversion error")
				}
			})
			if errors.Unwrap(field) != nil {
				t.Fatal("FieldError must not retain an underlying conversion error")
			}
		})
	}
}

func TestReaderJoinsAllSafeFieldErrorsInReadOrder(t *testing.T) {
	reader := config.FromMap(map[string]string{
		"PORT": "private-value", "ENABLED": "private-value", "TIMEOUT": "private-value",
	})
	reader.Required("SECRET")
	reader.Int("PORT", 80)
	reader.Bool("ENABLED", false)
	reader.Duration("TIMEOUT", time.Second)
	var got []config.FieldError
	walkConfigErrors(reader.Err(), func(err error) {
		if field, ok := err.(*config.FieldError); ok {
			got = append(got, *field)
		}
	})
	want := []config.FieldError{
		{Key: "SECRET", Rule: "required"},
		{Key: "PORT", Rule: "integer"},
		{Key: "ENABLED", Rule: "boolean"},
		{Key: "TIMEOUT", Rule: "duration"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("joined fields = %#v, want %#v", got, want)
	}
	if strings.Contains(reader.Err().Error(), "private-value") {
		t.Fatal("joined error disclosed a supplied value")
	}
}

func TestReaderFieldErrorChangesPreserveValidAndDefaultBehavior(t *testing.T) {
	for _, values := range []map[string]string{nil, {"PORT": "", "ENABLED": "", "TIMEOUT": ""}} {
		reader := config.FromMap(values)
		if reader.Int("PORT", 8080) != 8080 || !reader.Bool("ENABLED", true) || reader.Duration("TIMEOUT", time.Second) != time.Second {
			t.Fatal("missing or empty value did not preserve the fallback")
		}
		if err := reader.Err(); err != nil {
			t.Fatalf("optional defaults accumulated an error: %v", err)
		}
	}
	reader := config.FromMap(map[string]string{
		"SECRET": " padded-secret ", "PORT": "-42", "ENABLED": "FALSE", "TIMEOUT": "1m30s",
	})
	if reader.Required("SECRET") != " padded-secret " || reader.Int("PORT", 8080) != -42 || reader.Bool("ENABLED", true) || reader.Duration("TIMEOUT", time.Second) != 90*time.Second {
		t.Fatal("valid typed values changed")
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("valid values accumulated an error: %v", err)
	}
}

func TestFieldErrorDoesNotEchoUnknownRule(t *testing.T) {
	problem := &config.FieldError{Key: "APP_SETTING", Rule: "untrusted-secret-value"}
	if got := problem.Error(); got != "APP_SETTING has an invalid value" {
		t.Fatalf("unknown rule error = %q", got)
	}
}

func walkConfigErrors(err error, visit func(error)) {
	if err == nil {
		return
	}
	visit(err)
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range wrapped.Unwrap() {
			walkConfigErrors(child, visit)
		}
	case interface{ Unwrap() error }:
		walkConfigErrors(wrapped.Unwrap(), visit)
	}
}
