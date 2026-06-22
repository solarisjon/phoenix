// Package notifiers defines the Notifier interface and the core notifier registry.
package notifiers

import (
	"context"
	"encoding/json"
	"time"
)

// NotifyMessage carries the rendered notification content to a Notifier.
type NotifyMessage struct {
	EventType   string    `json:"event_type"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	TaskID      string    `json:"task_id"`
	TaskTitle   string    `json:"task_title"`
	AgentName   string    `json:"agent_name"`
	ProjectName string    `json:"project_name"`
	Error       string    `json:"error"`
	Timestamp   time.Time `json:"timestamp"`
}

// JSONSchema is a minimal JSON Schema descriptor used by the Settings UI
// to render a dynamic configuration form for a notifier plugin.
type JSONSchema struct {
	Type       string                `json:"type"`
	Properties map[string]SchemaField `json:"properties"`
	Required   []string              `json:"required,omitempty"`
}

// SchemaField describes a single config field for the UI.
type SchemaField struct {
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Secret      bool     `json:"secret,omitempty"` // hint for the UI to mask the field
}

// Notifier is the interface that core notification plugins implement.
type Notifier interface {
	// Send delivers a notification. The message body is pre-rendered
	// from a template — the notifier just delivers it.
	Send(ctx context.Context, cfg json.RawMessage, msg NotifyMessage) error

	// ValidateConfig checks that a config blob is well-formed before
	// saving to the database. Returns a user-friendly error string, or
	// nil if the config is valid.
	ValidateConfig(cfg json.RawMessage) error

	// ConfigSchema returns a JSON Schema describing the config fields.
	// The Settings UI renders this as a dynamic form.
	ConfigSchema() JSONSchema

	// TestMessage returns a test notification message for the /test endpoint.
	TestMessage() NotifyMessage
}

// Registry maps notifier kind strings to their implementations.
var Registry = map[string]Notifier{}

// Register adds a notifier to the global registry. Called by init() in each
// notifier package.
func Register(kind string, n Notifier) {
	Registry[kind] = n
}

// Get returns the notifier for the given kind, or nil if not registered.
func Get(kind string) Notifier {
	return Registry[kind]
}
