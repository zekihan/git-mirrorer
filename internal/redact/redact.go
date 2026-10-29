package redact

import "regexp"

var (
	userinfo = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/@]+@`)
	query    = regexp.MustCompile(`(https?://[^\s?#]+)[?#][^\s]*`)
	// Consume whole quoted values, including escaped quotes and whitespace.
	// The optional closing quote also covers a diagnostic cut off mid-value.
	secret     = regexp.MustCompile(`(?i)((?:authorization|password|token|secret)["']?\s*[=:]\s*)(?:(")(?:\\[\s\S]|[^"\\])*"?|(')(?:\\[\s\S]|[^'\\])*'?|[^\s]+)`)
	authHeader = regexp.MustCompile(`(?i)(authorization:\s*)(?:basic|bearer)\s+[^\s]+`)
)

func Text(value string) string {
	value = authHeader.ReplaceAllString(value, "${1}[redacted]")
	value = userinfo.ReplaceAllString(value, "${1}[redacted]@")
	value = query.ReplaceAllString(value, "${1}?[redacted]")
	// Preserve value quotes so later redaction passes retain adjacent fields.
	return secret.ReplaceAllString(value, "${1}${2}${3}[redacted]${2}${3}")
}
