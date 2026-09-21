package redact

import (
	"strings"
	"testing"
)

func TestDiagnosticCredentials(t *testing.T) {
	for _, input := range []string{
		"fatal: https://user:SUPERSECRET@example.com/repo",
		"https://example.com/repo?access_token=SUPERSECRET",
		"Authorization: Bearer SUPERSECRET", "Authorization: Basic SUPERSECRET",
		"password=SUPERSECRET token: SUPERSECRET secret=SUPERSECRET",
	} {
		if got := Text(input); strings.Contains(got, "SUPERSECRET") {
			t.Fatalf("secret leaked: %s", got)
		}
	}
}
