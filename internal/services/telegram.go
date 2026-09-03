package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
)

// telegramResponse is the response from Telegram sendMessage / editMessageText API.
type telegramResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
	Result      struct {
		MessageID int `json:"message_id"`
	} `json:"result"`
}

// SendTelegramAlert sends a markdown-formatted alert message to a Telegram chat.
// Returns the message_id from Telegram or an error.
func SendTelegramAlert(token, chatID, topicID, message string) (int, error) {
	if token == "" || chatID == "" {
		return 0, fmt.Errorf("token or chatID is empty")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       message,
		"parse_mode": "Markdown",
	}

	if topicID != "" {
		if threadID, err := strconv.Atoi(topicID); err == nil {
			payload["message_thread_id"] = threadID
		} else {
			payload["message_thread_id"] = topicID
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal payload: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return 0, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var tresp telegramResponse
	_ = json.Unmarshal(respBody, &tresp)

	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram sendMessage returned %d: %s", resp.StatusCode, string(respBody))
		if tresp.Description != "" {
			return 0, fmt.Errorf("%s (code %d)", tresp.Description, resp.StatusCode)
		}
		return 0, fmt.Errorf("telegram api returned %d: %s", resp.StatusCode, string(respBody))
	}

	if !tresp.OK {
		if tresp.Description != "" {
			return 0, fmt.Errorf("%s", tresp.Description)
		}
		return 0, fmt.Errorf("telegram api ok=false")
	}

	return tresp.Result.MessageID, nil
}

// EditTelegramMessage edits an existing message in a Telegram chat.
// Use after an agent reconnects to update a "disconnected" alert to "recovered".
// messageID is the ID returned by SendTelegramAlert.
func EditTelegramMessage(token, chatID, topicID string, messageID int, newText string) error {
	if token == "" || chatID == "" {
		return fmt.Errorf("token or chatID is empty")
	}
	if messageID == 0 {
		return fmt.Errorf("messageID is zero")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageText", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       newText,
		"parse_mode": "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram editMessageText returned %d: %s", resp.StatusCode, string(respBody))
		return fmt.Errorf("telegram api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var tresp telegramResponse
	if err := json.Unmarshal(respBody, &tresp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	if !tresp.OK {
		return fmt.Errorf("telegram api ok=false")
	}

	log.Printf("Telegram: edited message %d successfully", messageID)
	return nil
}

