package utils

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	passphrase := "super-secure-merchant-key-123"
	original := []byte(`{"tokens":{"accessToken":"secret-token","refreshToken":"secret-refresh"}}`)

	encrypted, err := Encrypt(original, passphrase)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if bytes.Equal(encrypted, original) {
		t.Fatalf("encrypted data matches original plaintext")
	}

	decrypted, err := Decrypt(encrypted, passphrase)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, original) {
		t.Fatalf("decrypted %s, want %s", string(decrypted), string(original))
	}
}

func TestDecryptWrongPassphraseFails(t *testing.T) {
	original := []byte("secret session data")
	encrypted, err := Encrypt(original, "correct-passphrase")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(encrypted, "wrong-passphrase")
	if err == nil {
		t.Fatalf("expected decryption with wrong passphrase to fail")
	}
}

func TestDecryptCorruptedCiphertextFails(t *testing.T) {
	original := []byte("secret session data")
	encrypted, _ := Encrypt(original, "passphrase")

	// Corrupt a byte in the tag/ciphertext
	encrypted[len(encrypted)-1] ^= 0xff

	_, err := Decrypt(encrypted, "passphrase")
	if err == nil {
		t.Fatalf("expected decryption with corrupted ciphertext to fail")
	}
}

