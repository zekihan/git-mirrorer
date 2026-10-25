package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zekihan/git-mirrorer/internal/mirror"
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("closed output") }

func TestReportOutputAndWriteFailure(t *testing.T) {
	value := mirror.Report{SchemaVersion: 1, Outcome: mirror.Failed, Sources: []mirror.SourceResult{{Name: "example", Destinations: []mirror.DestinationResult{{Name: "backup", Outcome: mirror.Skipped, Error: &mirror.Failure{Stage: "fetch", Code: "fetch", Detail: "unreachable"}}}}}}
	value.Error = &mirror.Failure{Stage: "run", Code: "timeout", Detail: "run deadline exceeded"}
	for _, format := range []string{"text", "json"} {
		var buffer bytes.Buffer
		if err := Write(&buffer, format, value); err != nil {
			t.Fatal(err)
		}
		if buffer.Len() == 0 {
			t.Fatal("empty report")
		}
		if !strings.Contains(buffer.String(), "run deadline exceeded") {
			t.Fatal("run-level failure was omitted")
		}
		if format == "json" && !json.Valid(buffer.Bytes()) {
			t.Fatal("invalid JSON")
		}
		if err := Write(brokenWriter{}, format, value); err == nil {
			t.Fatal("lost report write error")
		}
	}
}

func TestUntrustedDetailsStayInOneTextRecord(t *testing.T) {
	detail := "token=SUPERSECRET\nforged: success\r\x1b[2J\x1b]0;title\a\x00\x7f\u0085\u202e\u2028\u2029\xff"
	failure := &mirror.Failure{Code: "fetch", Detail: detail}
	value := mirror.Report{Outcome: mirror.Failed, Error: failure, Sources: []mirror.SourceResult{{
		Name: "source", Outcome: mirror.Failed, Error: failure,
		Destinations: []mirror.DestinationResult{{Name: "destination", Outcome: mirror.Failed, Error: failure}},
	}}}
	var buffer bytes.Buffer
	if err := Write(&buffer, "text", value); err != nil {
		t.Fatal(err)
	}
	got := buffer.String()
	if strings.Count(got, "\n") != 4 || strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("forged record or secret in report: %q", got)
	}
	for _, escape := range []string{`\nforged: success\r`, `\x1b[2J`, `\x1b]0;title\a`, `\x00`, `\x7f`, `\u0085`, `\u202e`, `\u2028`, `\u2029`, `\xff`} {
		if strings.Count(got, escape) != 3 {
			t.Errorf("missing visible escape %q: %q", escape, got)
		}
	}
}

func TestJSONPreservesStructuredDiagnostic(t *testing.T) {
	detail := "multiline\n\r\x1b[2J\x1b]0;title\a\x00\x7f\u0085\u202e"
	value := mirror.Report{Error: &mirror.Failure{Detail: detail}}
	var buffer bytes.Buffer
	if err := Write(&buffer, "json", value); err != nil {
		t.Fatal(err)
	}
	var decoded mirror.Report
	if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Error.Detail != detail || bytes.Contains(buffer.Bytes(), []byte{0x1b}) {
		t.Fatal("JSON changed diagnostic value or emitted a literal ESC")
	}
}
