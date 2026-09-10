package qris

import (
	"testing"
)

// buildStaticQris builds a synthetic static QRIS payload with a valid CRC,
// avoiding any real credential in the test.
func buildStaticQris(t *testing.T) string {
	t.Helper()
	m := EmvTlvMap{
		TagPayloadFormat:     "01",
		TagPointOfInitiation: POIStatic,
		"26":                 "0012BR.COM.GOPAY00111111111111111111",
		"52":                 "5912",
		"53":                 "360",
		"58":                 "ID",
		"59":                 "MERCHANT",
		"60":                 "Bandung",
		"61":                 "40123",
	}
	payload, err := BuildEmv(m)
	if err != nil {
		t.Fatalf("BuildEmv: %v", err)
	}
	return payload
}

func TestStaticToDynamicQrisInjectsAmount(t *testing.T) {
	static := buildStaticQris(t)
	dynamic, err := StaticToDynamicQris(static, 10001)
	if err != nil {
		t.Fatalf("StaticToDynamicQris: %v", err)
	}

	m, err := ParseEmv(dynamic)
	if err != nil {
		t.Fatalf("ParseEmv(dynamic): %v", err)
	}

	if got := m[TagTransactionAmount]; got != "10001" {
		t.Fatalf("tag 54 = %q, want %q", got, "10001")
	}
	if got := m[TagPointOfInitiation]; got != POIDynamic {
		t.Fatalf("tag 01 = %q, want %q", got, POIDynamic)
	}
}

func TestStaticToDynamicQrisValidChecksum(t *testing.T) {
	static := buildStaticQris(t)
	dynamic, err := StaticToDynamicQris(static, 5000)
	if err != nil {
		t.Fatalf("StaticToDynamicQris: %v", err)
	}
	if !IsValidQRISChecksum(dynamic) {
		t.Fatal("dynamic QRIS has invalid checksum")
	}
}

func TestStaticToDynamicQrisRejectsBadChecksum(t *testing.T) {
	static := buildStaticQris(t)
	// Corrupt a byte in the merchant name. We know "MERCHANT" appears in the
	// payload (tag 59 value). Replacing it with "XERCHANT" keeps the payload
	// parseable but breaks the CRC.
	corrupted := replaceOnce(static, "MERCHANT", "XERCHANT")
	if corrupted == static {
		t.Fatal("corruption did not alter the payload")
	}
	if !IsValidQRISChecksum(static) {
		t.Fatal("static payload should have valid checksum")
	}
	if IsValidQRISChecksum(corrupted) {
		t.Fatal("expected corrupted payload to fail checksum")
	}
	if _, err := StaticToDynamicQris(corrupted, 1000); err == nil {
		t.Fatal("expected error for corrupted payload, got nil")
	}
}

func replaceOnce(s, old, new string) string {
	idx := 0
	for i := 0; i < len(s)-len(old)+1; i++ {
		if s[i:i+len(old)] == old {
			idx = i
			break
		}
	}
	return s[:idx] + new + s[idx+len(old):]
}

func TestStaticToDynamicQrisRejectsNonPositiveAmount(t *testing.T) {
	static := buildStaticQris(t)
	if _, err := StaticToDynamicQris(static, 0); err == nil {
		t.Fatal("expected error for zero amount")
	}
	if _, err := StaticToDynamicQris(static, -5); err == nil {
		t.Fatal("expected error for negative amount")
	}
}

func TestParseEmvRejectsTruncated(t *testing.T) {
	// A tag claiming 99 bytes but the payload ends early.
	truncated := "0058" // tag 00 length 58 but no value
	if _, err := ParseEmv(truncated); err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

func TestEncodeTLVRejectsOversizedValue(t *testing.T) {
	value := make([]byte, 100)
	for i := range value {
		value[i] = 'x'
	}
	if _, err := EncodeTLV("59", string(value)); err == nil {
		t.Fatal("expected error for >99-byte value")
	}
}
