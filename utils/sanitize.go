package utils

import (
	"regexp"
)

var sensitiveKeyPattern = regexp.MustCompile(`"(?i)(access_token|refresh_token|otp_token|password|otp|secret|cookie|session)"\s*:\s*"[^"]*"`)

// SanitizeHTTPBody redacts sensitive tokens and truncates oversized HTTP response bodies
// to prevent credential or PII leaks in error objects and logs.
func SanitizeHTTPBody(body []byte, maxLen int) string {
	if len(body) == 0 {
		return ""
	}
	s := sensitiveKeyPattern.ReplaceAllString(string(body), `"$1":"[REDACTED]"`)
	if maxLen > 0 && len(s) > maxLen {
		return s[:maxLen] + "... (truncated)"
	}
	return s
}

