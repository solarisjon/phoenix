// Package telegram implements the Telegram Bot API notifier for Phoenix.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	// Expand ${ENV_VAR} in bot_token.
	token := provider.ExpandEnv(c.BotToken)
	if token == "" || token == c.BotToken && len(c.BotToken) > 0 && c.BotToken[0] == '$' {
		return fmt.Errorf("telegram: bot_token is empty or unresolved env var")
	}

	parseMode := c.ParseMode
	if parseMode == "" {
		parseMode = "Markdown"
	}

	body, _ := json.Marshal(map[string]string{
		"chat_id":    c.ChatID,
		"text":       msg.Body,
		"parse_mode": parseMode,
	})

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
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
