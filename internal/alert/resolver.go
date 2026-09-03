package alert

import (
	"strings"

	"monitoring-master/internal/models"
)

type TelegramTarget struct {
	Token    string
	ChatID   string
	TopicID  string
	IsGlobal bool
}

// TargetMessageID pairs a TelegramTarget with a sent MessageID for recovery editing.
type TargetMessageID struct {
	Target    TelegramTarget
	MessageID int
}

// ResolveTelegramTargets determines which Telegram destinations should receive alerts for an institution.
func ResolveTelegramTargets(inst *models.Institution, global *models.GlobalSetting) []TelegramTarget {
	var targets []TelegramTarget

	if inst == nil {
		if global != nil && global.EnableTelegramAlerts && strings.TrimSpace(global.TelegramBotToken) != "" && strings.TrimSpace(global.TelegramChatID) != "" {
			targets = append(targets, TelegramTarget{
				Token:    strings.TrimSpace(global.TelegramBotToken),
				ChatID:   strings.TrimSpace(global.TelegramChatID),
				TopicID:  strings.TrimSpace(global.TelegramTopicID),
				IsGlobal: true,
			})
		}
		return targets
	}

	// 1. If institution uses custom Telegram
	if inst.UseCustomTelegram {
		if !inst.EnableTelegramAlerts {
			return nil // Disabled explicitly for this institution
		}

		customToken := strings.TrimSpace(inst.TelegramBotToken)
		customChat := strings.TrimSpace(inst.TelegramChatID)
		if customToken != "" && customChat != "" {
			targets = append(targets, TelegramTarget{
				Token:    customToken,
				ChatID:   customChat,
				TopicID:  strings.TrimSpace(inst.TelegramTopicID),
				IsGlobal: false,
			})
		}

		// If Mirror to Global is active, add global target as well
		if inst.MirrorToGlobal && global != nil && global.EnableTelegramAlerts {
			globalToken := strings.TrimSpace(global.TelegramBotToken)
			globalChat := strings.TrimSpace(global.TelegramChatID)
			if globalToken != "" && globalChat != "" {
				targets = append(targets, TelegramTarget{
					Token:    globalToken,
					ChatID:   globalChat,
					TopicID:  strings.TrimSpace(global.TelegramTopicID),
					IsGlobal: true,
				})
			}
		}

		return targets
	}

	// 2. Fallback to Global
	if global != nil && global.EnableTelegramAlerts {
		globalToken := strings.TrimSpace(global.TelegramBotToken)
		globalChat := strings.TrimSpace(global.TelegramChatID)
		if globalToken != "" && globalChat != "" {
			targets = append(targets, TelegramTarget{
				Token:    globalToken,
				ChatID:   globalChat,
				TopicID:  strings.TrimSpace(global.TelegramTopicID),
				IsGlobal: true,
			})
		}
	}

	return targets
}
