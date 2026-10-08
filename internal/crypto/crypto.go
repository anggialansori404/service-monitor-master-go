// Package crypto provides AES-256-GCM authenticated encryption for
// sensitive values stored in the database (Telegram bot tokens, agent
// secret keys).
//
// Encrypted values are stored with an "enc:v1:" prefix so readers can
// distinguish ciphertext from legacy plaintext rows and migrate lazily.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Prefix marks values encrypted by this package (versioned for future rotation).
const Prefix = "enc:v1:"

// Cipher encrypts and decrypts strings with AES-256-GCM.
type Cipher struct {
	gcm cipher.AEAD
}

// NewCipher creates a Cipher from a 32-byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new GCM: %w", err)
	}
	return &Cipher{gcm: gcm}, nil
}

// Encrypt returns Prefix + base64(nonce || ciphertext).
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if c == nil {
		return "", errors.New("crypto: nil cipher")
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: nonce: %w", err)
	}
	ct := c.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return Prefix + base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt reverses Encrypt. Values without the Prefix are returned unchanged
// so rows written before encryption was enabled keep working (lazy migration).
func (c *Cipher) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, Prefix) {
		return value, nil
	}
	if c == nil {
		return "", errors.New("crypto: nil cipher cannot decrypt")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil {
		return "", fmt.Errorf("crypto: base64: %w", err)
	}
	nonceSize := c.gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("crypto: ciphertext too short")
	}
	nonce, ct := raw[:nonceSize], raw[nonceSize:]
	pt, err := c.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}
	return string(pt), nil
}

// IsEncrypted reports whether value carries the encryption prefix.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, Prefix)
}

// LoadKey resolves the 32-byte encryption key.
// Priority: MASTER_ENCRYPTION_KEY env (64 hex chars | base64 | passphrase),
// then ENCRYPTION_KEY_FILE (default data/.encryption_key, 0600).
// If neither exists, a random key is generated, persisted with 0600, and a
// warning is logged — better than plaintext, but operators should set the env
// var in production so the key survives reinstalls and is backed up.
func LoadKey() ([]byte, error) {
	if v := strings.TrimSpace(os.Getenv("MASTER_ENCRYPTION_KEY")); v != "" {
		return deriveKey(v)
	}
	path := strings.TrimSpace(os.Getenv("ENCRYPTION_KEY_FILE"))
	if path == "" {
		path = filepath.Join("data", ".encryption_key")
	}
	if data, err := os.ReadFile(path); err == nil {
		return deriveKey(strings.TrimSpace(string(data)))
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("crypto: generate key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("crypto: mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("crypto: persist key: %w", err)
	}
	fmt.Fprintf(os.Stderr, "WARNING: MASTER_ENCRYPTION_KEY not set; generated random key at %s (0600). Set MASTER_ENCRYPTION_KEY in production and back it up — losing it makes encrypted secrets unrecoverable.\n", path)
	return key, nil
}

// deriveKey accepts 64 hex chars, base64-encoded 32 bytes, or falls back to
// SHA-256 of the passphrase.
func deriveKey(v string) ([]byte, error) {
	if len(v) == 64 {
		if b, err := hex.DecodeString(v); err == nil {
			return b, nil
		}
	}
	if b, err := base64.StdEncoding.DecodeString(v); err == nil && len(b) == 32 {
		return b, nil
	}
	sum := sha256.Sum256([]byte(v))
	return sum[:], nil
}
