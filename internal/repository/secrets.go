package repository

import (
	"crypto/subtle"
	"log"

	"monitoring-master/internal/crypto"
	"monitoring-master/internal/models"
)

// This file centralizes at-rest encryption of credential fields
// (agent SecretKey, Telegram bot tokens) stored in the database.
//
// Rules:
//   - Writes encrypt via encryptSecrets*; an encryption failure aborts the
//     write — we never silently persist plaintext when a cipher is configured.
//   - Reads decrypt via decryptSecrets*; values without the "enc:v1:" prefix
//     are legacy plaintext and pass through unchanged (lazy migration).
//   - A nil cipher disables everything (tests / deployments without a key).
//   - Decryption failure yields "" (fail closed) and is logged.

func encryptValue(c *crypto.Cipher, v string) (string, error) {
	if c == nil || v == "" || crypto.IsEncrypted(v) {
		return v, nil
	}
	return c.Encrypt(v)
}

func decryptValue(c *crypto.Cipher, v, ctx string) string {
	if c == nil || v == "" || !crypto.IsEncrypted(v) {
		return v
	}
	pt, err := c.Decrypt(v)
	if err != nil {
		log.Printf("WARNING: failed to decrypt %s: %v (returning empty, fail closed)", ctx, err)
		return ""
	}
	return pt
}

func encryptInstitutionSecrets(c *crypto.Cipher, inst *models.Institution) error {
	var err error
	if inst.SecretKey, err = encryptValue(c, inst.SecretKey); err != nil {
		return err
	}
	if inst.TelegramBotToken, err = encryptValue(c, inst.TelegramBotToken); err != nil {
		return err
	}
	return nil
}

func decryptInstitutionSecrets(c *crypto.Cipher, inst *models.Institution) {
	inst.SecretKey = decryptValue(c, inst.SecretKey, "institution.secret_key")
	inst.TelegramBotToken = decryptValue(c, inst.TelegramBotToken, "institution.telegram_bot_token")
}

func encryptSettingSecrets(c *crypto.Cipher, s *models.GlobalSetting) error {
	var err error
	if s.TelegramBotToken, err = encryptValue(c, s.TelegramBotToken); err != nil {
		return err
	}
	return nil
}

func decryptSettingSecrets(c *crypto.Cipher, s *models.GlobalSetting) {
	s.TelegramBotToken = decryptValue(c, s.TelegramBotToken, "global_setting.telegram_bot_token")
}

// secretsMatch compares a stored (possibly encrypted) secret against the
// candidate provided by the agent, in constant time.
func secretsMatch(c *crypto.Cipher, stored, candidate string) bool {
	plain := decryptValue(c, stored, "auth.secret_key")
	if plain == "" || candidate == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(plain), []byte(candidate)) == 1
}
