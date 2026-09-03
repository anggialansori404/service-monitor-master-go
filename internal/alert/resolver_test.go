package alert_test

import (
	"testing"

	"monitoring-master/internal/alert"
	"monitoring-master/internal/models"
)

func TestResolveTelegramTargets(t *testing.T) {
	global := &models.GlobalSetting{
		TelegramBotToken:     "global-token",
		TelegramChatID:       "global-chat",
		TelegramTopicID:      "10",
		EnableTelegramAlerts: true,
	}

	t.Run("Fallback to Global when Custom is disabled", func(t *testing.T) {
		inst := &models.Institution{
			UseCustomTelegram: false,
		}
		targets := alert.ResolveTelegramTargets(inst, global)
		if len(targets) != 1 {
			t.Fatalf("expected 1 target, got %d", len(targets))
		}
		if targets[0].Token != "global-token" || !targets[0].IsGlobal {
			t.Errorf("expected global target, got %+v", targets[0])
		}
	})

	t.Run("Exclusive Custom Target when MirrorToGlobal is false", func(t *testing.T) {
		inst := &models.Institution{
			UseCustomTelegram:    true,
			TelegramBotToken:     "custom-token",
			TelegramChatID:       "custom-chat",
			TelegramTopicID:      "20",
			EnableTelegramAlerts: true,
			MirrorToGlobal:       false,
		}
		targets := alert.ResolveTelegramTargets(inst, global)
		if len(targets) != 1 {
			t.Fatalf("expected 1 target, got %d", len(targets))
		}
		if targets[0].Token != "custom-token" || targets[0].IsGlobal {
			t.Errorf("expected custom target, got %+v", targets[0])
		}
	})

	t.Run("Dual Target (Custom + Global) when MirrorToGlobal is true", func(t *testing.T) {
		inst := &models.Institution{
			UseCustomTelegram:    true,
			TelegramBotToken:     "custom-token",
			TelegramChatID:       "custom-chat",
			TelegramTopicID:      "20",
			EnableTelegramAlerts: true,
			MirrorToGlobal:       true,
		}
		targets := alert.ResolveTelegramTargets(inst, global)
		if len(targets) != 2 {
			t.Fatalf("expected 2 targets, got %d", len(targets))
		}
		if targets[0].Token != "custom-token" || targets[1].Token != "global-token" {
			t.Errorf("expected [custom, global], got %+v", targets)
		}
	})

	t.Run("No Targets when Alerts are Disabled", func(t *testing.T) {
		inst := &models.Institution{
			UseCustomTelegram:    true,
			TelegramBotToken:     "custom-token",
			TelegramChatID:       "custom-chat",
			EnableTelegramAlerts: false,
			MirrorToGlobal:       true,
		}
		targets := alert.ResolveTelegramTargets(inst, global)
		if len(targets) != 0 {
			t.Fatalf("expected 0 targets, got %d", len(targets))
		}
	})
}
