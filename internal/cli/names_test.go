package cli

import "testing"

func TestGeneratorNamesPreserveInitialisms(t *testing.T) {
	for _, test := range []struct{ input, exported, file string }{
		{"HTTPRequest", "HTTPRequest", "http_request"},
		{"api_client", "APIClient", "api_client"},
		{"user ID", "UserID", "user_id"},
		{"Issue", "Issue", "issue"},
	} {
		t.Run(test.input, func(t *testing.T) {
			if got, err := pascal(test.input); err != nil || got != test.exported {
				t.Fatalf("pascal = %q, %v; want %q", got, err, test.exported)
			}
			if got, err := snake(test.input); err != nil || got != test.file {
				t.Fatalf("snake = %q, %v; want %q", got, err, test.file)
			}
		})
	}
}
