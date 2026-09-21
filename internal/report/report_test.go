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
