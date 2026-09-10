package shopee

import (
	"testing"
)

func TestMD5Hex(t *testing.T) {
	// MD5("hello") = 5d41402abc4b2a76b9719d911017c592
	got := MD5Hex("hello")
	want := "5d41402abc4b2a76b9719d911017c592"
	if got != want {
		t.Fatalf("MD5Hex(hello) = %q, want %q", got, want)
	}
}

func TestSHA256Hex(t *testing.T) {
	// SHA-256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	got := SHA256Hex("hello")
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("SHA256Hex(hello) = %q, want %q", got, want)
	}
}

func TestHashShopeePassword(t *testing.T) {
	// hashShopeePassword = SHA256(lowercase MD5 hex of password).
	md5Hex := MD5Hex("secret")
	want := SHA256Hex(md5Hex)
	got := HashShopeePassword("secret")
	if got != want {
		t.Fatalf("HashShopeePassword = %q, want %q", got, want)
	}
	// Must be lowercase hex of 64 chars.
	if len(got) != 64 {
		t.Fatalf("HashShopeePassword length = %d, want 64", len(got))
	}
}
