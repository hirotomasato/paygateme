package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

const (
	saltSize   = 16
	nonceSize  = 12
	iterations = 10000
)

var (
	ErrCiphertextTooShort = errors.New("ciphertext too short")
	ErrDecryptionFailed   = errors.New("decryption failed: invalid passphrase or corrupted data")
)

// deriveKey stretches passphrase and salt into a 32-byte key using iterated SHA-256.
func deriveKey(passphrase string, salt []byte) []byte {
	key := sha256.Sum256(append([]byte(passphrase), salt...))
	for i := 1; i < iterations; i++ {
		key = sha256.Sum256(key[:])
	}
	return key[:]
}

// Encrypt encrypts plaintext using AES-256-GCM with a key derived from passphrase.
// Output format: [16 bytes salt][12 bytes nonce][ciphertext + 16 bytes auth tag].
func Encrypt(plaintext []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("passphrase cannot be empty")
	}

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	key := deriveKey(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, plaintext, nil)

	result := make([]byte, 0, len(salt)+len(nonce)+len(sealed))
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, sealed...)
	return result, nil
}

// Decrypt decrypts AES-256-GCM data using the passphrase.
func Decrypt(ciphertext []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("passphrase cannot be empty")
	}

	minLen := saltSize + nonceSize + 16 // 16 is GCM tag size
	if len(ciphertext) < minLen {
		return nil, ErrCiphertextTooShort
	}

	salt := ciphertext[:saltSize]
	nonce := ciphertext[saltSize : saltSize+nonceSize]
	encrypted := ciphertext[saltSize+nonceSize:]

	key := deriveKey(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}

