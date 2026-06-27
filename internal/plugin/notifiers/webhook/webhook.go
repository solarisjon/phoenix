// Package webhook implements a generic HTTP webhook notifier for Phoenix.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/solarisjon/phoenix/internal/plugin/notifiers"
	"github.com/solarisjon/phoenix/internal/provider"
)

func init() {
	notifiers.Register("webhook", &Notifier{})
}

// Config holds the webhook notifier configuration.
type Config struct {
	URL            string `json:"url"`
	AuthHeader     string `json:"auth_header"`
	Secret         string `json:"secret"`          // HMAC-SHA256 signing secret
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// WebhookPayload is the JSON body sent to the webhook endpoint.
type WebhookPayload struct {
	Event     string    `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	Task      struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	} `json:"task"`
	Agent struct {
		Name string `json:"name"`
	} `json:"agent"`
	Project struct {
		Name string `json:"name"`
	} `json:"project"`
	Message string `json:"message"`
}

// Notifier sends notifications as HTTP POST requests to a configured URL.
type Notifier struct{}

func (n *Notifier) Send(ctx context.Context, cfg json.RawMessage, msg notifiers.NotifyMessage) error {
	var c Config
	if err := json.Unmarshal(cfg, &c); err != nil {
		return fmt.Errorf("webhook: invalid config: %w", err)
	}

	timeout := time.Duration(c.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	payload := WebhookPayload{
		Event:     msg.EventType,
		Timestamp: msg.Timestamp,
		Message:   msg.Body,
	}
	payload.Task.ID = msg.TaskID
	payload.Task.Title = msg.TaskTitle
	payload.Task.Status = msg.EventType // simplified; caller sets the right value
	payload.Agent.Name = msg.AgentName
	payload.Project.Name = msg.ProjectName

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook: marshal payload: %w", err)
	}

	url := provider.ExpandEnv(c.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Apply auth header if configured.
	if c.AuthHeader != "" {
		expanded := provider.ExpandEnv(c.AuthHeader)
		parts := strings.SplitN(expanded, ":", 2)
		if len(parts) == 2 {
			req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}

	// Sign the payload with HMAC-SHA256 when a secret is configured.
	// Recipients can verify using the X-Phoenix-Signature header: sha256=<hex>.
	if c.Secret != "" {
		secret := provider.ExpandEnv(c.Secret)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set("X-Phoenix-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("webhook: endpoint returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (n *Notifier) ValidateConfig(cfg json.RawMessage) error {
	var c Config
	if err := json.Unmarshal(cfg, &c); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if c.URL == "" {
		return fmt.Errorf("url is required")
	}
	if !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") && !strings.HasPrefix(c.URL, "${") {
		return fmt.Errorf("url must start with http:// or https://")
	}
	return nil
}

func (n *Notifier) ConfigSchema() notifiers.JSONSchema {
	return notifiers.JSONSchema{
		Type: "object",
		Properties: map[string]notifiers.SchemaField{
			"url": {
				Type:        "string",
				Title:       "Webhook URL",
				Description: "The HTTP endpoint to POST notifications to. Supports ${ENV_VAR} syntax.",
			},
			"auth_header": {
				Type:        "string",
				Title:       "Auth Header",
				Description: "Optional HTTP header for authentication (e.g. 'Authorization: Bearer ${TOKEN}'). Supports ${ENV_VAR} syntax.",
				Secret:      true,
			},
			"secret": {
				Type:        "string",
				Title:       "Signing Secret",
				Description: "Optional shared secret for HMAC-SHA256 request signing. Phoenix sends X-Phoenix-Signature: sha256=<hex> so receivers can verify authenticity. Supports ${ENV_VAR} syntax.",
				Secret:      true,
			},
			"timeout_seconds": {
				Type:        "integer",
				Title:       "Timeout (seconds)",
				Description: "Request timeout in seconds.",
				Default:     10,
			},
		},
		Required: []string{"url"},
	}
}

func (n *Notifier) TestMessage() notifiers.NotifyMessage {
	return notifiers.NotifyMessage{
		EventType:   "test",
		Title:       "Phoenix Test",
		Body:        "Phoenix plugin test — Webhook is configured correctly.",
		TaskTitle:   "Test Notification",
		AgentName:   "System",
		ProjectName: "Phoenix",
		Timestamp:   time.Now(),
	}
}
