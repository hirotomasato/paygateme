package utils

import (
	"strings"
	"testing"
)

func TestSanitizeHTTPBodyRedactsTokens(t *testing.T) {
	input := []byte(`{"error":"invalid_grant","access_token":"secret123","otp":"987654","user_id":123}`)
	got := SanitizeHTTPBody(input, 500)

	if strings.Contains(got, "secret123") {
		t.Errorf("SanitizeHTTPBody leaked access_token value")
	}
	if strings.Contains(got, "987654") {
		t.Errorf("SanitizeHTTPBody leaked otp value")
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in sanitized output")
	}
}

func TestSanitizeHTTPBodyTruncatesOversized(t *testing.T) {
	longBody := []byte(strings.Repeat("A", 1000))
	got := SanitizeHTTPBody(longBody, 100)

	if len(got) > 130 { // 100 + len("... (truncated)")
		t.Errorf("expected truncated body, got len=%d", len(got))
	}
	if !strings.HasSuffix(got, "... (truncated)") {
		t.Errorf("expected truncated suffix")
	}
}

