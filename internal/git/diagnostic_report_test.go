package git_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zekihan/git-mirrorer/internal/git"
	"github.com/zekihan/git-mirrorer/internal/mirror"
	"github.com/zekihan/git-mirrorer/internal/report"
)

func TestTruncatedCommandDiagnosticsInReports(t *testing.T) {
	runner := &git.Runner{Executable: "/bin/sh", Timeout: 5 * time.Second, Env: os.Environ()}
	for _, record := range []string{
		"https://user:SUPERSECRET@example.com/repo",
		"https://example.com/repo?access_token=SUPERSECRET",
		"Authorization: Bearer SUPERSECRET", "Authorization: Basic SUPERSECRET",
		"password=SUPERSECRET", "token: SUPERSECRET", "secret=SUPERSECRET",
	} {
		for cut := 0; cut <= len(record); cut++ {
			suffix := "\nsafe record\n" + strings.Repeat("x", 32*1024-len(record)+cut-len("\nsafe record\n"))
			err := runner.Run(t.Context(), "", strings.NewReader(record+suffix), nil, "-c", "cat >&2; exit 1")
			var commandErr *git.CommandError
			if !errors.As(err, &commandErr) {
				t.Fatalf("expected command failure: %v", err)
			}
			if cut > 0 && !strings.HasPrefix(commandErr.Detail, "safe record\n") {
				t.Fatalf("record %q cut %d retained partial credential record", record, cut)
			}
			value := mirror.Report{Error: &mirror.Failure{Detail: commandErr.Detail}}
			for _, format := range []string{"text", "json"} {
				var output bytes.Buffer
				if err := report.Write(&output, format, value); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(output.String(), "SUPERSECRET") {
					t.Fatalf("record %q cut %d leaked in %s", record, cut, format)
				}
			}
		}
	}
}

func TestLongSingleLineCommandDiagnosticIsOmitted(t *testing.T) {
	runner := &git.Runner{Executable: "/bin/sh", Timeout: 5 * time.Second, Env: os.Environ()}
	input := "Authorization: Bearer " + strings.Repeat("SUPERSECRET", 4000)
	err := runner.Run(t.Context(), "", strings.NewReader(input), nil, "-c", "cat >&2; exit 1")
	var commandErr *git.CommandError
	if !errors.As(err, &commandErr) || commandErr.Detail != "" {
		t.Fatal("long credential record was retained")
	}
}

func TestQuotedCommandCredentialsInReports(t *testing.T) {
	runner := &git.Runner{Executable: "/bin/sh", Timeout: 5 * time.Second, Env: os.Environ()}
	for _, input := range []string{
		`remote: {"token":"SUPERSECRET SECONDSECRET", "error":"denied"}`,
		`remote: {"token":"SUPERSECRET SECONDSECRET","error":"denied"}`,
		`remote: password="SUPERSECRET SECONDSECRET" denied`,
		`remote: Authorization: "Bearer SUPERSECRET SECONDSECRET" denied`,
	} {
		err := runner.Run(t.Context(), "", strings.NewReader(input), nil, "-c", "cat >&2; exit 1")
		var commandErr *git.CommandError
		if !errors.As(err, &commandErr) {
			t.Fatalf("expected command failure: %v", err)
		}
		if strings.Contains(err.Error(), "SUPERSECRET") || strings.Contains(err.Error(), "SECONDSECRET") {
			t.Fatalf("command error leaked credentials: %v", err)
		}
		value := mirror.Report{Error: &mirror.Failure{Detail: commandErr.Detail}}
		for _, format := range []string{"text", "json"} {
			var output bytes.Buffer
			if err := report.Write(&output, format, value); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), "SUPERSECRET") || strings.Contains(output.String(), "SECONDSECRET") {
				t.Fatalf("credentials leaked in %s: %s", format, output.String())
			}
			if !strings.Contains(output.String(), "denied") {
				t.Fatalf("unrelated diagnostic lost in %s", format)
			}
		}
	}
}
