// Package telegram implements the Telegram Bot API notifier for Phoenix.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

	// Inbound task creation settings.
	InboundEnabled   bool   `json:"inbound_enabled"`
	DefaultProjectID string `json:"default_project_id"`
	DefaultAgentID   string `json:"default_agent_id"`
}

// InboundHandler is called when a text message arrives from the configured chat.
// The returned string (if non-empty) is sent back as a confirmation reply.
type InboundHandler func(ctx context.Context, text string) (reply string, err error)

// StatusHandler is called when a /status command arrives.
// Returns a Markdown-formatted summary to send back to the chat.
type StatusHandler func(ctx context.Context) (string, error)

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
	slog.Debug("telegram: sending to chat", "chat_id", chatID, "token_len", len(token))
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
		slog.Error("telegram: send failed", "chat_id", chatID, "status", resp.StatusCode)
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
		// inbound_enabled, default_project_id, default_agent_id are handled
		// by the frontend as a dedicated Telegram inbound section (not schema-driven).
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

// StartPoller begins a long-polling loop for inbound messages from Telegram.
// It blocks until ctx is cancelled. On startup it drains any pending updates
// (messages sent before the poller started) so they are not replayed as tasks.
// For each new text message from the configured chat_id, handler is called and
// the returned reply (if non-empty) is sent back to the chat.
// statusFn, if non-nil, is called when the user sends /status.
func StartPoller(ctx context.Context, cfg Config, handler InboundHandler, statusFn StatusHandler) {
	token := strings.TrimSpace(provider.ExpandEnv(cfg.BotToken))
	if token == "" || strings.HasPrefix(token, "${") {
		slog.Warn("telegram poller: bot_token is empty or unresolved — not starting")
		return
	}
	allowedChatID := strings.TrimSpace(cfg.ChatID)

	client := &http.Client{Timeout: 35 * time.Second}

	// Drain any pending updates first so we don't replay stale messages.
	offset := drainPendingUpdates(ctx, client, token)
	slog.Info("telegram poller: started", "offset", offset, "allowed_chat", allowedChatID)

	for {
		select {
		case <-ctx.Done():
			slog.Info("telegram poller: stopping")
			return
		default:
		}

		url := fmt.Sprintf(
			"https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=30&allowed_updates=[\"message\"]",
			token, offset,
		)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			slog.Error("telegram poller: build request", "error", err)
			sleepOrDone(ctx, 5*time.Second)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("telegram poller: getUpdates error", "error", err)
			sleepOrDone(ctx, 5*time.Second)
			continue
		}

		var result struct {
			OK     bool `json:"ok"`
			Result []struct {
				UpdateID int64 `json:"update_id"`
				Message  *struct {
					Date int64  `json:"date"`
					Text string `json:"text"`
					Chat struct {
						ID int64 `json:"id"`
					} `json:"chat"`
				} `json:"message"`
			} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			slog.Error("telegram poller: decode", "error", err)
			sleepOrDone(ctx, 5*time.Second)
			continue
		}
		resp.Body.Close()

		if !result.OK {
			slog.Warn("telegram poller: Telegram API returned ok=false")
			sleepOrDone(ctx, 10*time.Second)
			continue
		}

		for _, upd := range result.Result {
			offset = upd.UpdateID + 1

			if upd.Message == nil {
				continue
			}

			// Security: only accept messages from the configured chat.
			chatStr := fmt.Sprintf("%d", upd.Message.Chat.ID)
			if allowedChatID != "" && chatStr != allowedChatID {
				slog.Warn("telegram poller: ignoring message from unauthorized chat", "chat", chatStr)
				continue
			}

			text := strings.TrimSpace(upd.Message.Text)
			if text == "" {
				continue
			}

			text = parseCommand(text)
			switch text {
			case "":
				// Unrecognised slash command — send a help hint.
				sendMessage(ctx, client, token, upd.Message.Chat.ID,
					"Send any text (or /task <description>) to create a Phoenix task. Use /status to see active tasks.", "Markdown")
				continue
			case "\x00status":
				if statusFn != nil {
					msg, err := statusFn(ctx)
					if err != nil {
						sendMessage(ctx, client, token, upd.Message.Chat.ID,
							fmt.Sprintf("❌ Status error: %s", err.Error()), "Markdown")
					} else {
						sendMessage(ctx, client, token, upd.Message.Chat.ID, msg, "Markdown")
					}
				} else {
					sendMessage(ctx, client, token, upd.Message.Chat.ID,
						"Status not available.", "Markdown")
				}
				continue
			}

			reply, err := handler(ctx, text)
			if err != nil {
				slog.Error("telegram poller: handler error", "error", err)
				sendMessage(ctx, client, token, upd.Message.Chat.ID,
					fmt.Sprintf("❌ Error: %s", err.Error()), "Markdown")
				continue
			}
			if reply != "" {
				sendMessage(ctx, client, token, upd.Message.Chat.ID, reply, "Markdown")
			}
		}
	}
}

// parseCommand normalises the incoming message text.
// Returns the task description, "\x00status" for /status, or "" to ignore.
func parseCommand(text string) string {
	lower := strings.ToLower(text)
	if lower == "/status" {
		return "\x00status"
	}
	for _, prefix := range []string{"/task ", "/run "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(text[len(prefix):])
		}
	}
	if text == "/task" || text == "/run" {
		return "" // bare command with no description — ignore
	}
	if strings.HasPrefix(text, "/") {
		return "" // unrecognised slash command
	}
	return text // plain text → use as-is
}

// drainPendingUpdates calls getUpdates with no wait timeout to consume any
// backlogged messages, advancing past them. Returns the next offset to use.
func drainPendingUpdates(ctx context.Context, client *http.Client, token string) int64 {
	url := fmt.Sprintf(
		"https://api.telegram.org/bot%s/getUpdates?limit=100&timeout=0&allowed_updates=[\"message\"]",
		token,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || !result.OK || len(result.Result) == 0 {
		return 0
	}

	last := result.Result[len(result.Result)-1].UpdateID
	return last + 1
}

// sendMessage sends a text message back to a Telegram chat.
func sendMessage(ctx context.Context, client *http.Client, token string, chatID int64, text, parseMode string) {
	if parseMode == "" {
		parseMode = "Markdown"
	}
	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": parseMode,
	})
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Error("telegram: sendMessage: build request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("telegram: sendMessage", "error", err)
		return
	}
	resp.Body.Close()
}

// sleepOrDone sleeps for d or until ctx is done.
func sleepOrDone(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
