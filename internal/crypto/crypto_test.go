package crypto

import (
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	for _, pt := range []string{"", "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11", "short", strings.Repeat("x", 1000)} {
		enc, err := c.Encrypt(pt)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", pt, err)
		}
		if !IsEncrypted(enc) {
			t.Fatalf("Encrypt(%q) missing prefix", pt)
		}
		dec, err := c.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if dec != pt {
			t.Fatalf("round trip mismatch: got %q want %q", dec, pt)
		}
	}
}

func TestDecryptPassthroughPlaintext(t *testing.T) {
	key := make([]byte, 32)
	c, _ := NewCipher(key)
	for _, pt := range []string{"", "plaintext-token", "enc:v0:notours"} {
		dec, err := c.Decrypt(pt)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", pt, err)
		}
		if dec != pt {
			t.Fatalf("passthrough mismatch: got %q want %q", dec, pt)
		}
	}
}

func TestDecryptTampered(t *testing.T) {
	key := make([]byte, 32)
	c, _ := NewCipher(key)
	enc, _ := c.Encrypt("secret")
	tampered := enc[:len(enc)-2] + "AA"
	if _, err := c.Decrypt(tampered); err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	k1 := make([]byte, 32)
	k2 := make([]byte, 32)
	k2[0] = 1
	c1, _ := NewCipher(k1)
	c2, _ := NewCipher(k2)
	enc, _ := c1.Encrypt("secret")
	if _, err := c2.Decrypt(enc); err == nil {
		t.Fatal("expected error for wrong key")
	}
}

func TestNewCipherBadKey(t *testing.T) {
	if _, err := NewCipher(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}

func TestDeriveKey(t *testing.T) {
	// 64 hex chars
	hexKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	k, err := deriveKey(hexKey)
	if err != nil || len(k) != 32 {
		t.Fatalf("hex derive: %v", err)
	}
	// passphrase fallback -> sha256
	k2, err := deriveKey("my-passphrase")
	if err != nil || len(k2) != 32 {
		t.Fatalf("passphrase derive: %v", err)
	}
	c, err := NewCipher(k2)
	if err != nil {
		t.Fatalf("NewCipher(passphrase key): %v", err)
	}
	enc, _ := c.Encrypt("x")
	if dec, _ := c.Decrypt(enc); dec != "x" {
		t.Fatal("passphrase key round trip failed")
	}
}
