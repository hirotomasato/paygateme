package shopee

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// MD5Hex computes the MD5 digest of input and returns it as lowercase hex.
func MD5Hex(input string) string {
	h := md5.Sum([]byte(input))
	return hex.EncodeToString(h[:])
}

// SHA256Hex computes the SHA-256 digest of input and returns it as lowercase hex.
func SHA256Hex(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

// HashShopeePassword formats a password for Shopee's wire: SHA-256 over the
// lowercase MD5 hex digest.
func HashShopeePassword(password string) string {
	return SHA256Hex(MD5Hex(password))
}

// DebugLog is a helper for debug logging without verbosity overhead.
func DebugLog(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
