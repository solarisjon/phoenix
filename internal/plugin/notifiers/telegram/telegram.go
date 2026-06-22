// Package telegram implements the Telegram Bot API notifier for Phoenix.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/solarisjon/phoenix/internal/plugin/notifiers"
	"github.com/solarisjon/phoenix/internal/provider"
)

func init() {
	notifiers.Register("telegram", &Notifier{})
}

// Config holds the Telegram notifier configuration.
type Config struct {
	BotToken  string `json:"bot_token"`
	ChatID    string `json:"chat_id"`
	ParseMode string `json:"parse_mode"`
}

// Notifier sends messages to a Telegram chat via the Bot API.
type Notifier struct{}

func (n *Notifier) Send(ctx context.Context, cfg json.RawMessage, msg notifiers.NotifyMessage) error {
	var c Config
	if err := json.Unmarshal(cfg, &c); err != nil {
		return fmt.Errorf("telegram: invalid config: %w", err)
	}

	// Expand ${ENV_VAR} in bot_token and trim whitespace.
	token := strings.TrimSpace(provider.ExpandEnv(c.BotToken))
	if token == "" {
		return fmt.Errorf("telegram: bot_token is empty")
	}
	if strings.HasPrefix(token, "${") {
		return fmt.Errorf("telegram: bot_token env var %q is not set in the environment", c.BotToken)
	}

	chatID := strings.TrimSpace(c.ChatID)
	if chatID == "" {
		return fmt.Errorf("telegram: chat_id is empty")
	}

	parseMode := c.ParseMode
	if parseMode == "" {
		parseMode = "Markdown"
	}

	body, _ := json.Marshal(map[string]string{
		"chat_id":    chatID,
		"text":       msg.Body,
		"parse_mode": parseMode,
	})

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	log.Printf("telegram: sending to chat %s (token length: %d)", chatID, len(token))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		// Mask the token in logs for safety — show first 5 and last 4 chars.
		masked := token
		if len(token) > 12 {
			masked = token[:5] + "…" + token[len(token)-4:]
		}
		log.Printf("telegram: failed — token=%q (length %d), chat_id=%q, status=%d", masked, len(token), chatID, resp.StatusCode)
		return fmt.Errorf("telegram: API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (n *Notifier) ValidateConfig(cfg json.RawMessage) error {
	var c Config
	if err := json.Unmarshal(cfg, &c); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if c.BotToken == "" {
		return fmt.Errorf("bot_token is required")
	}
	if c.ChatID == "" {
		return fmt.Errorf("chat_id is required")
	}
	if c.ParseMode != "" && c.ParseMode != "Markdown" && c.ParseMode != "HTML" {
		return fmt.Errorf("parse_mode must be 'Markdown' or 'HTML'")
	}
	return nil
}

func (n *Notifier) ConfigSchema() notifiers.JSONSchema {
	return notifiers.JSONSchema{
		Type: "object",
		Properties: map[string]notifiers.SchemaField{
			"bot_token": {
				Type:        "string",
				Title:       "Bot Token",
				Description: "Telegram Bot API token from @BotFather. Supports ${ENV_VAR} syntax.",
				Secret:      true,
			},
			"chat_id": {
				Type:        "string",
				Title:       "Chat ID",
				Description: "Numeric chat ID or @channel_name to send messages to.",
			},
			"parse_mode": {
				Type:        "string",
				Title:       "Parse Mode",
				Description: "Message formatting mode.",
				Default:     "Markdown",
				Enum:        []string{"Markdown", "HTML"},
			},
		},
		Required: []string{"bot_token", "chat_id"},
	}
}

// ChatInfo holds a discovered chat from the Telegram getUpdates API.
type ChatInfo struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`      // group name or empty for DMs
	FirstName string `json:"first_name"` // user first name for DMs
	Type      string `json:"type"`       // "private", "group", "supergroup", "channel"
}

// GetChats calls the Telegram getUpdates API and returns all unique chats
// that have sent messages to the bot. The user must have messaged the bot
// at least once (e.g. /start) for their chat to appear.
func GetChats(botToken string) ([]ChatInfo, error) {
	token := strings.TrimSpace(provider.ExpandEnv(botToken))
	if token == "" || strings.HasPrefix(token, "${") {
		return nil, fmt.Errorf("bot_token is empty or env var is not set")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", token)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("getUpdates request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			Message *struct {
				Chat struct {
					ID        int64  `json:"id"`
					Title     string `json:"title"`
					FirstName string `json:"first_name"`
					Type      string `json:"type"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode getUpdates: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("Telegram API error — check your bot token")
	}

	// Deduplicate by chat ID.
	seen := map[int64]bool{}
	var chats []ChatInfo
	for _, u := range result.Result {
		if u.Message == nil {
			continue
		}
		c := u.Message.Chat
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		chats = append(chats, ChatInfo{
			ID:        c.ID,
			Title:     c.Title,
			FirstName: c.FirstName,
			Type:      c.Type,
		})
	}

	if len(chats) == 0 {
		return nil, fmt.Errorf("no chats found — send /start to the bot first, then try again")
	}

	return chats, nil
}

func (n *Notifier) TestMessage() notifiers.NotifyMessage {
	return notifiers.NotifyMessage{
		EventType:   "test",
		Title:       "Phoenix Test",
		Body:        "✅ Phoenix plugin test — Telegram is configured correctly.",
		TaskTitle:   "Test Notification",
		AgentName:   "System",
		ProjectName: "Phoenix",
		Timestamp:   time.Now(),
	}
}
