// Package plugin provides the plugin manager for Phoenix.
// It coordinates plugin lifecycle, event dispatch, and notification delivery.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/plugin/notifiers"
	"github.com/solarisjon/phoenix/internal/plugin/notifiers/telegram"
	"github.com/solarisjon/phoenix/internal/store"

	// Import notifier packages for their init() registration side-effects.
	_ "github.com/solarisjon/phoenix/internal/plugin/notifiers/telegram"
	_ "github.com/solarisjon/phoenix/internal/plugin/notifiers/webhook"
)

// InboundTaskFunc is the callback registered by main.go to create a task from
// an inbound Telegram message. Returns the new task ID or an error.
type InboundTaskFunc func(ctx context.Context, projectID, agentID, title, source string) (taskID string, err error)

// ManagerOpts holds startup configuration for the plugin manager.
type ManagerOpts struct {
	NoPlugins bool // --no-plugins CLI flag: disables all dispatch
}

// Manager coordinates plugin lifecycle, event dispatch, and notification delivery.
type Manager struct {
	plugins  store.PluginRepo
	rules    store.NotificationRuleRepo
	settings store.SystemSettingsRepo

	noPlugins bool // runtime override from CLI flag

	mu                      sync.RWMutex
	corePluginsEnabled      bool
	communityPluginsEnabled bool

	// Inbound polling state.
	pollerParentCtx context.Context
	pollers         map[string]context.CancelFunc // pluginID → cancel
	inboundHandler  InboundTaskFunc
}

// NewManager creates a plugin manager. Call SeedCorePlugins and LoadAll after construction.
func NewManager(
	plugins store.PluginRepo,
	rules store.NotificationRuleRepo,
	settings store.SystemSettingsRepo,
	opts ManagerOpts,
) *Manager {
	return &Manager{
		plugins:   plugins,
		rules:     rules,
		settings:  settings,
		noPlugins: opts.NoPlugins,
		pollers:   make(map[string]context.CancelFunc),
	}
}

// NoPluginsFlag returns true if the --no-plugins runtime flag is active.
func (m *Manager) NoPluginsFlag() bool {
	return m.noPlugins
}

// SeedCorePlugins ensures core plugin records exist in the database.
// Called once at startup. Existing records are not modified.
func (m *Manager) SeedCorePlugins(ctx context.Context) error {
	corePlugins := []model.Plugin{
		{
			ID:     "core-telegram",
			Name:   "Telegram",
			Type:   model.PluginTypeNotifier,
			Kind:   "telegram",
			IsCore: true,
			Enabled: false, // disabled until user configures
			Config: `{}`,
		},
		{
			ID:     "core-webhook",
			Name:   "Webhook",
			Type:   model.PluginTypeNotifier,
			Kind:   "webhook",
			IsCore: true,
			Enabled: false,
			Config: `{}`,
		},
	}

	for _, cp := range corePlugins {
		existing, err := m.plugins.Get(ctx, cp.ID)
		if err != nil {
			return fmt.Errorf("seed core plugin %s: %w", cp.ID, err)
		}
		if existing == nil {
			if err := m.plugins.Create(ctx, &cp); err != nil {
				return fmt.Errorf("create core plugin %s: %w", cp.ID, err)
			}
			log.Printf("plugin: seeded core plugin %q (%s)", cp.Name, cp.Kind)
		}
	}
	return nil
}

// LoadAll refreshes the master switch state from system settings.
func (m *Manager) LoadAll(ctx context.Context) error {
	settings, err := m.settings.Get(ctx)
	if err != nil {
		return fmt.Errorf("plugin: load settings: %w", err)
	}
	m.mu.Lock()
	m.corePluginsEnabled = settings.CorePluginsEnabled
	m.communityPluginsEnabled = settings.CommunityPluginsEnabled
	m.mu.Unlock()
	log.Printf("plugin: loaded master switches — core=%v, community=%v", settings.CorePluginsEnabled, settings.CommunityPluginsEnabled)
	return nil
}

// RefreshSettings reloads the master switch state. Called after settings are updated.
func (m *Manager) RefreshSettings(ctx context.Context) error {
	return m.LoadAll(ctx)
}

// isPluginActive checks all three enable/disable levels.
func (m *Manager) isPluginActive(p *model.Plugin) bool {
	if m.noPlugins {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p.IsCore && !m.corePluginsEnabled {
		return false
	}
	if !p.IsCore && !m.communityPluginsEnabled {
		return false
	}
	return p.Enabled
}

// HandleEvent is the callback wired to the WebSocket hub via OnEvent.
// It maps task status changes to notification event types and dispatches
// to all matching notifier plugins.
func (m *Manager) HandleEvent(eventType string, payload json.RawMessage) {
	if m.noPlugins {
		return
	}

	// Only process task.status_changed events.
	if eventType != "task.status_changed" {
		return
	}

	// Parse the status payload.
	var sp struct {
		TaskID    string  `json:"task_id"`
		AgentID   string  `json:"agent_id"`
		ProjectID string  `json:"project_id"`
		Status    string  `json:"status"`
		CostUSD   float64 `json:"cost_usd"`
		Title     string  `json:"title"`
	}
	if err := json.Unmarshal(payload, &sp); err != nil {
		log.Printf("plugin: unmarshal status payload: %v", err)
		return
	}

	// Map task status to notification event type.
	var notifyEvent model.NotifyEventType
	switch model.TaskStatus(sp.Status) {
	case model.TaskStatusCompleted:
		notifyEvent = model.NotifyTaskCompleted
	case model.TaskStatusFailed:
		notifyEvent = model.NotifyTaskFailed
	case model.TaskStatusAwaitingApproval:
		notifyEvent = model.NotifyNeedsApproval
		// Note: guardrail_triggered is a refinement — we'd need guardrail_reason
		// from the task record to distinguish. For v1, awaiting_approval covers it.
	default:
		return // not a notifiable status
	}

	// Dispatch in background.
	go m.dispatch(notifyEvent, sp.TaskID, sp.Title, sp.AgentID, sp.ProjectID)
}

// dispatch finds matching rules and sends notifications.
func (m *Manager) dispatch(eventType model.NotifyEventType, taskID, taskTitle, agentID, projectID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rules, err := m.rules.ListByEventType(ctx, eventType)
	if err != nil {
		log.Printf("plugin: list rules for %s: %v", eventType, err)
		return
	}

	for _, rule := range rules {
		// Project filter: nil = all projects.
		if rule.ProjectID != nil && *rule.ProjectID != projectID {
			continue
		}

		plugin, err := m.plugins.Get(ctx, rule.PluginID)
		if err != nil || plugin == nil {
			log.Printf("plugin: get plugin %s: %v", rule.PluginID, err)
			continue
		}

		if !m.isPluginActive(plugin) {
			continue
		}

		notifier := notifiers.Get(plugin.Kind)
		if notifier == nil {
			log.Printf("plugin: no notifier registered for kind %q", plugin.Kind)
			continue
		}

		// Build the notification message.
		msg := notifiers.NotifyMessage{
			EventType:   string(eventType),
			TaskID:      taskID,
			TaskTitle:   taskTitle,
			AgentName:   agentID,   // UUID for now; name lookup would require repo injection
			ProjectName: projectID, // UUID for now; name lookup would require repo injection
			Timestamp:   time.Now(),
		}

		// Render the message body from template.
		tmplText := defaultTemplate(eventType)
		if rule.Template != nil && *rule.Template != "" {
			tmplText = *rule.Template
		}
		msg.Body = renderTemplate(tmplText, msg)
		msg.Title = msg.Body // for now, title = body

		// Send asynchronously per notifier. Each goroutine gets its own
		// independent context so that the parent dispatch() returning (and
		// cancelling its ctx via defer) does not abort the in-flight HTTP call.
		go func(p *model.Plugin, n notifiers.Notifier, m notifiers.NotifyMessage) {
			sendCtx, sendCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer sendCancel()
			if err := n.Send(sendCtx, json.RawMessage(p.Config), m); err != nil {
				log.Printf("plugin: %s send failed: %v", p.Kind, err)
			} else {
				log.Printf("plugin: %s notification sent for %s", p.Kind, m.EventType)
			}
		}(plugin, notifier, msg)
	}
}

// defaultTemplate returns the built-in Go text/template for the given event type.
func defaultTemplate(eventType model.NotifyEventType) string {
	switch eventType {
	case model.NotifyTaskFailed:
		return "🔴 Task Failed: {{.TaskTitle}}\nAgent: {{.AgentName}}\nProject: {{.ProjectName}}\nError: {{.Error}}"
	case model.NotifyTaskCompleted:
		return "✅ Task Completed: {{.TaskTitle}}\nAgent: {{.AgentName}}\nProject: {{.ProjectName}}"
	case model.NotifyNeedsApproval:
		return "⏳ Approval Needed: {{.TaskTitle}}\nAgent: {{.AgentName}}\nProject: {{.ProjectName}}"
	case model.NotifyGuardrailFired:
		return "⚠️ Guardrail Triggered: {{.TaskTitle}}\nAgent: {{.AgentName}}\nProject: {{.ProjectName}}\nReason: {{.Error}}"
	default:
		return "Phoenix notification: {{.TaskTitle}}"
	}
}

// renderTemplate safely renders a Go text/template with the given message data.
func renderTemplate(tmpl string, msg notifiers.NotifyMessage) string {
	t, err := template.New("notify").Parse(tmpl)
	if err != nil {
		log.Printf("plugin: parse template: %v", err)
		return fmt.Sprintf("%s: %s", msg.EventType, msg.TaskTitle)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, msg); err != nil {
		log.Printf("plugin: execute template: %v", err)
		return fmt.Sprintf("%s: %s", msg.EventType, msg.TaskTitle)
	}
	return buf.String()
}

// ---- Inbound polling ----

// SetInboundHandler registers the callback used to create tasks from inbound Telegram messages.
// Must be called before StartPollers.
func (m *Manager) SetInboundHandler(fn InboundTaskFunc) {
	m.mu.Lock()
	m.inboundHandler = fn
	m.mu.Unlock()
}

// StartPollers launches long-polling goroutines for all enabled Telegram plugins
// that have inbound_enabled=true. The provided ctx controls their lifetime;
// cancel it (or call StopPollers) to shut everything down.
func (m *Manager) StartPollers(ctx context.Context) {
	if m.noPlugins {
		return
	}
	m.mu.Lock()
	m.pollerParentCtx = ctx
	m.mu.Unlock()

	plugins, err := m.plugins.List(ctx)
	if err != nil {
		log.Printf("plugin: StartPollers: list plugins: %v", err)
		return
	}
	for _, p := range plugins {
		if p.Kind == "telegram" {
			m.startPoller(p)
		}
	}
}

// StopPollers cancels all running poller goroutines.
func (m *Manager) StopPollers() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.pollers {
		cancel()
		delete(m.pollers, id)
	}
}

// RestartPoller stops any existing poller for the given plugin and starts a
// new one if the plugin is still eligible (enabled, telegram kind, inbound on).
// Safe to call from HTTP handlers — uses the parent context stored by StartPollers.
func (m *Manager) RestartPoller(pluginID string) {
	m.mu.Lock()
	ctx := m.pollerParentCtx
	m.mu.Unlock()
	if ctx == nil {
		return // StartPollers not called yet
	}

	// Stop any running poller for this plugin.
	m.mu.Lock()
	if cancel, ok := m.pollers[pluginID]; ok {
		cancel()
		delete(m.pollers, pluginID)
	}
	m.mu.Unlock()

	// Re-fetch plugin and restart if still eligible.
	p, err := m.plugins.Get(ctx, pluginID)
	if err != nil || p == nil {
		return // plugin deleted or error — nothing to start
	}
	m.startPoller(p)
}

// startPoller launches a poller goroutine for p if it qualifies.
// Must NOT be called with m.mu held.
func (m *Manager) startPoller(p *model.Plugin) {
	if m.noPlugins || !m.isPluginActive(p) {
		return
	}

	var cfg telegram.Config
	if err := json.Unmarshal([]byte(p.Config), &cfg); err != nil {
		log.Printf("plugin: telegram startPoller %s: unmarshal config: %v", p.ID, err)
		return
	}
	if !cfg.InboundEnabled {
		return
	}
	if cfg.DefaultProjectID == "" || cfg.DefaultAgentID == "" {
		log.Printf("plugin: telegram inbound %s: default_project_id or default_agent_id not set — skipping", p.ID)
		return
	}

	m.mu.Lock()
	parentCtx := m.pollerParentCtx
	if parentCtx == nil {
		m.mu.Unlock()
		return
	}
	pollerCtx, cancel := context.WithCancel(parentCtx)
	m.pollers[p.ID] = cancel
	m.mu.Unlock()

	pluginID := p.ID
	projectID := cfg.DefaultProjectID
	agentID := cfg.DefaultAgentID

	handler := func(ctx context.Context, text string) (string, error) {
		m.mu.RLock()
		fn := m.inboundHandler
		m.mu.RUnlock()
		if fn == nil {
			return "", fmt.Errorf("inbound handler not configured")
		}

		title := text
		const maxTitle = 200
		if len([]rune(title)) > maxTitle {
			runes := []rune(title)
			title = string(runes[:maxTitle]) + "…"
		}

		_, err := fn(ctx, projectID, agentID, title, fmt.Sprintf("telegram:%s", pluginID))
		if err != nil {
			return fmt.Sprintf("❌ Failed to create task: %s", err.Error()), nil
		}
		return fmt.Sprintf("✅ Task queued: *%s*", escapeMarkdown(title)), nil
	}

	go func() {
		log.Printf("plugin: telegram poller starting for plugin %s", pluginID)
		telegram.StartPoller(pollerCtx, cfg, handler)
		log.Printf("plugin: telegram poller stopped for plugin %s", pluginID)
	}()
}

// escapeMarkdown escapes Telegram MarkdownV1 special characters.
func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"`", "\\`",
	)
	return replacer.Replace(s)
}

// TestPlugin sends a test notification through the given plugin.
func (m *Manager) TestPlugin(ctx context.Context, pluginID string) error {
	plugin, err := m.plugins.Get(ctx, pluginID)
	if err != nil {
		return fmt.Errorf("get plugin: %w", err)
	}
	if plugin == nil {
		return fmt.Errorf("plugin not found")
	}

	notifier := notifiers.Get(plugin.Kind)
	if notifier == nil {
		return fmt.Errorf("no notifier registered for kind %q", plugin.Kind)
	}

	msg := notifier.TestMessage()
	return notifier.Send(ctx, json.RawMessage(plugin.Config), msg)
}
