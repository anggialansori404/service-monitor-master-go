package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// SendTelegramAlert sends a markdown-formatted alert message to a Telegram chat.
func SendTelegramAlert(token, chatID, topicID, message string) {
	if token == "" || chatID == "" {
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       message,
		"parse_mode": "Markdown",
	}

	if topicID != "" {
		payload["message_thread_id"] = topicID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal telegram payload: %v", err)
		return
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("Failed to send telegram alert: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram API returned non-200 status: %d", resp.StatusCode)
	}
}

