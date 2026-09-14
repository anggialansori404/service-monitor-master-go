package repository

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoInstitutionDocToModel(t *testing.T) {
	oid := bson.NewObjectID()
	now := time.Now()
	future := now.Add(2 * time.Hour)

	doc := &mongoInstitutionDoc{
		ID:                   oid,
		Name:                 "Test Institution",
		AgentID:              "agent-xyz",
		SecretKey:            "secret-123",
		IsActive:             true,
		LastPing:             &now,
		UseCustomTelegram:    true,
		TelegramBotToken:     "bot:token",
		TelegramChatID:       "-100123456",
		TelegramTopicID:      "42",
		EnableTelegramAlerts: true,
		MirrorToGlobal:       false,
		InMaintenance:        true,
		MaintenanceUntil:     &future,
	}

	model := doc.toModel()

	if model.ID != oid.Hex() {
		t.Errorf("expected ID %s, got %s", oid.Hex(), model.ID)
	}
	if model.Name != "Test Institution" {
		t.Errorf("expected Name 'Test Institution', got '%s'", model.Name)
	}
	if !model.InMaintenance {
		t.Errorf("expected InMaintenance to be true, got false")
	}
	if model.MaintenanceUntil == nil || *model.MaintenanceUntil != future {
		t.Errorf("expected MaintenanceUntil %v, got %v", future, model.MaintenanceUntil)
	}
	if !model.UseCustomTelegram {
		t.Errorf("expected UseCustomTelegram to be true, got false")
	}
	if model.TelegramBotToken != "bot:token" {
		t.Errorf("expected TelegramBotToken 'bot:token', got '%s'", model.TelegramBotToken)
	}
	if model.TelegramTopicID != "42" {
		t.Errorf("expected TelegramTopicID '42', got '%s'", model.TelegramTopicID)
	}
	if !model.NotifyNodeOffline {
		t.Errorf("expected NotifyNodeOffline to default to true when nil in doc, got false")
	}

	// Test with explicit false for NotifyNodeOffline
	falseVal := false
	docWithFalse := &mongoInstitutionDoc{
		ID:                "test-false",
		NotifyNodeOffline: &falseVal,
	}
	if docWithFalse.toModel().NotifyNodeOffline {
		t.Errorf("expected NotifyNodeOffline to be false, got true")
	}

	// Test with string ID as fallback
	docStringID := &mongoInstitutionDoc{
		ID:            "custom-string-id",
		Name:          "String ID Inst",
		InMaintenance: false,
	}
	modelStr := docStringID.toModel()
	if modelStr.ID != "custom-string-id" {
		t.Errorf("expected ID 'custom-string-id', got '%s'", modelStr.ID)
	}
	if modelStr.InMaintenance {
		t.Errorf("expected InMaintenance to be false, got true")
	}
}
