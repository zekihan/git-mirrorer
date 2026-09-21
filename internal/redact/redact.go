package redact

import "regexp"

var (
	userinfo   = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/@]+@`)
	query      = regexp.MustCompile(`(https?://[^\s?#]+)[?#][^\s]*`)
	secret     = regexp.MustCompile(`(?i)((?:authorization|password|token|secret)[=:]\s*)[^\s]+`)
	authHeader = regexp.MustCompile(`(?i)(authorization:\s*)(?:basic|bearer)\s+[^\s]+`)
)

func Text(value string) string {
	value = authHeader.ReplaceAllString(value, "${1}[redacted]")
	value = userinfo.ReplaceAllString(value, "${1}[redacted]@")
	value = query.ReplaceAllString(value, "${1}?[redacted]")
	return secret.ReplaceAllString(value, "${1}[redacted]")
}
