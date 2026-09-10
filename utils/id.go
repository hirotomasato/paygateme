// Package utils provides runtime-agnostic helpers used across the gateway.
package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

// PaymentID generates a short, url-safe, time-ordered identifier for payments.
// Combines a base36 timestamp with random entropy so ids sort roughly by
// creation time.
func PaymentID() string {
	timePart := time.Now().UnixMilli()
	randPart := randomBase36(6)
	return fmt.Sprintf("pay_%s%s", base36Encode(timePart), randPart)
}

// UUID generates a random UUID v4 string.
func UUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

const base36Chars = "0123456789abcdefghijklmnopqrstuvwxyz"

func base36Encode(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = base36Chars[n%36]
		n /= 36
	}
	return string(buf[i:])
}

func randomBase36(n int) string {
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(36))
		b[i] = base36Chars[idx.Int64()]
	}
	return string(b)
}