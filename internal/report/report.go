package report

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/zekihan/git-mirrorer/internal/mirror"
	"github.com/zekihan/git-mirrorer/internal/redact"
)

func Write(writer io.Writer, format string, result mirror.Report) error {
	if format == "json" {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	if result.Error != nil {
		if _, err := fmt.Fprintf(writer, "run: %s (%s: %s)\n", result.Outcome, result.Error.Code, redact.Text(result.Error.Detail)); err != nil {
			return err
		}
	}
	for _, source := range result.Sources {
		if source.Error != nil {
			if _, err := fmt.Fprintf(writer, "%s: %s (%s: %s)\n", source.Name, source.Outcome, source.Error.Code, redact.Text(source.Error.Detail)); err != nil {
				return err
			}
		}
		for _, destination := range source.Destinations {
			changes := destination.Changes
			if _, err := fmt.Fprintf(writer, "%s -> %s: %s [%s] (+%d ~%d -%d =%d rejected=%d)", source.Name, destination.Name, destination.Outcome,
				destination.Duration.Round(time.Millisecond), changes.Created, changes.Updated, changes.Deleted, changes.Unchanged, changes.Rejected); err != nil {
				return err
			}
			if destination.Error != nil {
				if _, err := fmt.Fprintf(writer, " %s: %s", destination.Error.Code, redact.Text(destination.Error.Detail)); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(writer, "%s: %d/%d destinations succeeded; %d failed, %d skipped, %d canceled; %s\n",
		result.Outcome, result.Totals.Succeeded, result.Totals.Destinations,
		result.Totals.Failed, result.Totals.Skipped, result.Totals.Canceled, result.Duration.Round(time.Millisecond))
	return err
}
