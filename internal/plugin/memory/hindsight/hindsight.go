// Package hindsight implements the memory.MemoryClient interface backed by
// the Hindsight agent memory system (https://github.com/vectorize-io/hindsight).
//
// API paths used:
//   POST   /v1/default/banks/{bank_id}/memories         — retain
//   POST   /v1/default/banks/{bank_id}/memories/recall  — recall
//   DELETE /v1/default/banks/{bank_id}/memories         — clear
//   GET    /v1/default/banks                            — ping
package hindsight

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config is stored as JSON in plugins.config.
type Config struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

// ConfigField describes one field in the plugin config form.
type ConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
}

// ConfigSchema returns the list of fields rendered by the plugin config editor.
func ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "base_url", Label: "Hindsight URL", Type: "text", Required: true, Placeholder: "http://localhost:8888"},
		{Key: "api_key", Label: "API Key", Type: "password", Required: false, Placeholder: "Optional"},
	}
}

// ValidateConfig checks that the stored config JSON is well-formed and has a base_url.
func ValidateConfig(raw []byte) error {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("hindsight: invalid config JSON: %w", err)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return fmt.Errorf("hindsight: base_url is required")
	}
	return nil
}

// Client is a Hindsight HTTP client implementing memory.MemoryClient.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New creates a Client from a config JSON blob.
func New(configJSON string) (*Client, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("hindsight: parse config: %w", err)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("hindsight: base_url is required")
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Retain stores content as a memory for agentID.
func (c *Client) Retain(ctx context.Context, agentID, content string) error {
	body := map[string]any{
		"items": []map[string]string{
			{"content": content},
		},
	}
	_, err := c.post(ctx, c.bankURL(agentID, "memories"), body)
	return err
}

// Recall retrieves memories relevant to query for agentID.
// Returns the recalled text, concatenated from all result items.
func (c *Client) Recall(ctx context.Context, agentID, query string) (string, error) {
	body := map[string]any{
		"query":  query,
		"budget": "mid",
	}
	resp, err := c.post(ctx, c.bankURL(agentID, "memories/recall"), body)
	if err != nil {
		return "", err
	}

	var result struct {
		Results []struct {
			Text string `json:"text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("hindsight: decode recall response: %w", err)
	}

	var parts []string
	for _, r := range result.Results {
		if r.Text != "" {
			parts = append(parts, r.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// ClearBank deletes all memories for agentID.
func (c *Client) ClearBank(ctx context.Context, agentID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.bankURL(agentID, "memories"), nil)
	if err != nil {
		return fmt.Errorf("hindsight: build clear request: %w", err)
	}
	c.setHeaders(req)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hindsight: clear bank: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("hindsight: clear bank %d: %s", res.StatusCode, string(b))
	}
	return nil
}

// Ping checks connectivity by listing banks.
func (c *Client) Ping(ctx context.Context) error {
	url := c.baseURL + "/v1/default/banks"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("hindsight: build ping request: %w", err)
	}
	c.setHeaders(req)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hindsight: ping: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf("hindsight: ping returned %d", res.StatusCode)
	}
	return nil
}

// bankURL constructs a URL for a bank-scoped path segment.
func (c *Client) bankURL(bankID, path string) string {
	return fmt.Sprintf("%s/v1/default/banks/%s/%s", c.baseURL, bankID, path)
}

// post sends a JSON POST and returns the raw response body.
func (c *Client) post(ctx context.Context, url string, body any) (json.RawMessage, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("hindsight: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("hindsight: build request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hindsight: %w", err)
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("hindsight: HTTP %d: %s", res.StatusCode, string(respBody))
	}
	return json.RawMessage(respBody), nil
}

// setHeaders adds the Authorization header when an API key is configured.
func (c *Client) setHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}
