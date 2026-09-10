package utils

import "testing"

func TestCRC16CCITTStandardVector(t *testing.T) {
	// CRC-16/CCITT-FALSE check value for "123456789" is 0x29B1.
	if got := CRC16CCITT("123456789"); got != "29B1" {
		t.Fatalf("CRC16CCITT(%q) = %q, want %q", "123456789", got, "29B1")
	}
}

func TestCRC16CCITTFEmpty(t *testing.T) {
	if got := CRC16CCITT(""); got != "FFFF" {
		t.Fatalf("CRC16CCITT(empty) = %q, want %q", got, "FFFF")
	}
}

func TestCRC16CCITTUpperCase(t *testing.T) {
	// Result must be uppercase hex.
	got := CRC16CCITT("abc")
	for _, r := range got {
		if r >= 'a' && r <= 'f' {
			t.Fatalf("CRC16CCITT returned lowercase hex: %q", got)
		}
	}
}
