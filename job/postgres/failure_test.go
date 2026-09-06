package postgres

import "testing"

func TestSafeFailureMessageRejectsArbitraryDiagnostics(t *testing.T) {
	tests := []struct {
		kind    string
		message string
		want    string
	}{
		{kind: "error", message: "postgres password=secret", want: "handler returned an error"},
		{kind: "error", message: "handler requested a retry", want: "handler requested a retry"},
		{kind: "panic", message: "panic: token=secret\ngoroutine stack", want: "handler panicked"},
		{kind: "permanent", message: "api key secret", want: "handler reported a permanent failure"},
		{kind: "custom", message: "credential secret", want: "job attempt failed"},
	}
	for _, test := range tests {
		if got := safeFailureMessage(test.kind, test.message); got != test.want {
			t.Errorf("safeFailureMessage(%q, %q) = %q, want %q", test.kind, test.message, got, test.want)
		}
	}
}

func TestSafeFailureMessageHonorsConfiguredBound(t *testing.T) {
	store := &Store{maxError: 7}
	if got := store.boundedSafeFailureMessage("panic", "secret"); got != "handler" {
		t.Fatalf("bounded safe message = %q", got)
	}
}
