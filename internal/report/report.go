package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
		if _, err := fmt.Fprintf(writer, "run: %s (%s: %s)\n", result.Outcome, result.Error.Code, textDetail(result.Error.Detail)); err != nil {
			return err
		}
	}
	for _, source := range result.Sources {
		sourceName := escapeText(source.Name)
		if source.Error != nil {
			if _, err := fmt.Fprintf(writer, "%s: %s (%s: %s)\n", sourceName, source.Outcome, source.Error.Code, textDetail(source.Error.Detail)); err != nil {
				return err
			}
		}
		for _, destination := range source.Destinations {
			changes := destination.Changes
			if _, err := fmt.Fprintf(writer, "%s -> %s: %s [%s] (+%d ~%d -%d =%d rejected=%d)", sourceName, escapeText(destination.Name), destination.Outcome,
				destination.Duration.Round(time.Millisecond), changes.Created, changes.Updated, changes.Deleted, changes.Unchanged, changes.Rejected); err != nil {
				return err
			}
			if destination.Error != nil {
				if _, err := fmt.Fprintf(writer, " %s: %s", destination.Error.Code, textDetail(destination.Error.Detail)); err != nil {
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

func textDetail(detail string) string {
	return escapeText(redact.Text(detail))
}

// escapeText keeps labels and diagnostics on one physical line and prevents
// terminals from interpreting their control characters.
func escapeText(value string) string {
	var output strings.Builder
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			output.WriteString(`\x`)
			output.WriteString(strconv.FormatUint(uint64(value[0])>>4, 16))
			output.WriteString(strconv.FormatUint(uint64(value[0])&15, 16))
		} else if r == '\\' {
			output.WriteString(`\\`)
		} else if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			// QuoteRuneToASCII supplies visible Go-style escapes without delimiters.
			quoted := strconv.QuoteRuneToASCII(r)
			output.WriteString(quoted[1 : len(quoted)-1])
		} else {
			output.WriteRune(r)
		}
		value = value[size:]
	}
	return output.String()
}
