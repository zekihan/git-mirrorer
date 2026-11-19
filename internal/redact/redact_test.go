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

func TestHTTPURLSuffixRedactionKeepsSchemeCase(t *testing.T) {
	for _, scheme := range []string{"http", "https", "HTTP", "HtTpS"} {
		t.Run(scheme, func(t *testing.T) {
			address := scheme + "://example.invalid/repo"
			want := "GET " + address + "?[redacted] failed"
			for _, suffix := range []string{"?view=summary", "#history"} {
				if got := Text("GET " + address + suffix + " failed"); got != want {
					t.Errorf("Text with URL suffix %q = %q, want %q", suffix, got, want)
				}
			}
		})
	}
}

func TestQuotedDiagnosticCredentials(t *testing.T) {
	for _, input := range []string{
		`remote: {"token":"SUPERSECRET SECONDSECRET", "error":"denied"}`,
		`remote: {"token":"SUPERSECRET SECONDSECRET","error":"denied"}`,
		`remote: {'secret': 'SUPERSECRET SECONDSECRET', 'error': 'denied'}`,
		`password="SUPERSECRET SECONDSECRET" denied`,
		`password = 'SUPERSECRET SECONDSECRET' denied`,
		`Authorization: "Bearer SUPERSECRET SECONDSECRET" denied`,
		`{"authorization": "Bearer SUPERSECRET SECONDSECRET"} denied`,
		`password="SUPERSECRET\" SECONDSECRET" denied`,
		`secret='SUPERSECRET\' SECONDSECRET' denied`,
		`token="SUPERSECRET\\ SECONDSECRET" denied`,
		`password="SUPERSECRET SECONDSECRET`,
	} {
		t.Run(input, func(t *testing.T) {
			got := Text(Text(input))
			if strings.Contains(got, "SUPERSECRET") || strings.Contains(got, "SECONDSECRET") {
				t.Fatalf("secret leaked: %s", got)
			}
			if strings.Contains(input, "denied") && !strings.Contains(got, "denied") {
				t.Fatalf("unrelated diagnostic lost: %s", got)
			}
		})
	}
}
