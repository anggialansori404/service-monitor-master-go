package repository

import (
	"testing"

	"monitoring-master/internal/crypto"
	"monitoring-master/internal/models"
)

func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, err := crypto.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

func TestEncryptDecryptInstitutionSecrets(t *testing.T) {
	c := testCipher(t)
	inst := &models.Institution{
		SecretKey:        "my-agent-secret",
		TelegramBotToken: "123456:ABC-DEF",
		TelegramChatID:   "-100123", // not a secret, must stay plaintext
	}
	if err := encryptInstitutionSecrets(c, inst); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !crypto.IsEncrypted(inst.SecretKey) {
		t.Error("SecretKey not encrypted")
	}
	if !crypto.IsEncrypted(inst.TelegramBotToken) {
		t.Error("TelegramBotToken not encrypted")
	}
	if crypto.IsEncrypted(inst.TelegramChatID) {
		t.Error("TelegramChatID should stay plaintext")
	}
	decryptInstitutionSecrets(c, inst)
	if inst.SecretKey != "my-agent-secret" {
		t.Errorf("SecretKey round trip failed: %q", inst.SecretKey)
	}
	if inst.TelegramBotToken != "123456:ABC-DEF" {
		t.Errorf("TelegramBotToken round trip failed: %q", inst.TelegramBotToken)
	}
}

func TestDecryptLegacyPlaintextPassthrough(t *testing.T) {
	c := testCipher(t)
	inst := &models.Institution{SecretKey: "legacy-plaintext", TelegramBotToken: ""}
	decryptInstitutionSecrets(c, inst)
	if inst.SecretKey != "legacy-plaintext" {
		t.Errorf("legacy plaintext should pass through: %q", inst.SecretKey)
	}
}

func TestEncryptIdempotent(t *testing.T) {
	c := testCipher(t)
	inst := &models.Institution{SecretKey: "s3cr3t"}
	if err := encryptInstitutionSecrets(c, inst); err != nil {
		t.Fatal(err)
	}
	first := inst.SecretKey
	if err := encryptInstitutionSecrets(c, inst); err != nil {
		t.Fatal(err)
	}
	if inst.SecretKey != first {
		t.Error("double encrypt changed value (not idempotent)")
	}
}

func TestSecretsMatch(t *testing.T) {
	c := testCipher(t)
	stored, err := c.Encrypt("correct-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !secretsMatch(c, stored, "correct-secret") {
		t.Error("should match")
	}
	if secretsMatch(c, stored, "wrong-secret") {
		t.Error("should not match wrong secret")
	}
	if secretsMatch(c, stored, "") {
		t.Error("should not match empty candidate")
	}
	// legacy plaintext stored value
	if !secretsMatch(c, "plain-secret", "plain-secret") {
		t.Error("legacy plaintext should match")
	}
}

func TestEncryptSettingSecrets(t *testing.T) {
	c := testCipher(t)
	s := &models.GlobalSetting{TelegramBotToken: "999:XYZ", TelegramChatID: "123"}
	if err := encryptSettingSecrets(c, s); err != nil {
		t.Fatal(err)
	}
	if !crypto.IsEncrypted(s.TelegramBotToken) {
		t.Error("token not encrypted")
	}
	decryptSettingSecrets(c, s)
	if s.TelegramBotToken != "999:XYZ" {
		t.Errorf("round trip failed: %q", s.TelegramBotToken)
	}
}
