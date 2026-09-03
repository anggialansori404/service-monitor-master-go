package models_test

import (
	"encoding/json"
	"testing"

	"monitoring-master/internal/models"
)

func TestInstitutionTelegramFieldsSerialization(t *testing.T) {
	inst := models.Institution{
		ID:                   "inst-123",
		Name:                 "RSUD Harapan",
		UseCustomTelegram:    true,
		TelegramBotToken:     "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
		TelegramChatID:       "-1001987654321",
		TelegramTopicID:      "42",
		EnableTelegramAlerts: true,
		MirrorToGlobal:       true,
	}

	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("failed to marshal institution: %v", err)
	}

	var decoded models.Institution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal institution: %v", err)
	}

	if decoded.UseCustomTelegram != true {
		t.Errorf("expected UseCustomTelegram true, got false")
	}
	if decoded.TelegramBotToken != inst.TelegramBotToken {
		t.Errorf("expected token %s, got %s", inst.TelegramBotToken, decoded.TelegramBotToken)
	}
	if decoded.TelegramChatID != inst.TelegramChatID {
		t.Errorf("expected chat id %s, got %s", inst.TelegramChatID, decoded.TelegramChatID)
	}
	if decoded.TelegramTopicID != "42" {
		t.Errorf("expected topic id 42, got %s", decoded.TelegramTopicID)
	}
	if decoded.MirrorToGlobal != true {
		t.Errorf("expected MirrorToGlobal true, got false")
	}
}
