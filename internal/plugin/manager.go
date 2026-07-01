// Package plugin provides the plugin manager for Phoenix.
// It coordinates plugin lifecycle, event dispatch, and notification delivery.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/plugin/memory"
	"github.com/solarisjon/phoenix/internal/plugin/memory/hindsight"
	"github.com/solarisjon/phoenix/internal/plugin/notifiers"
	"github.com/solarisjon/phoenix/internal/plugin/notifiers/telegram"
	"github.com/solarisjon/phoenix/internal/store"

	// Import webhook notifier for its init() registration side-effect.
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
	agents   store.AgentRepo
	projects store.ProjectRepo

	noPlugins bool // runtime override from CLI flag

	mu                      sync.RWMutex
	corePluginsEnabled      bool
	communityPluginsEnabled bool

	// Cached memory client (nil = disabled or not configured).
	memClient memory.MemoryClient

	// Inbound polling state.
	pollerParentCtx context.Context
	pollers         map[string]context.CancelFunc // pluginID → cancel
	inboundHandler  InboundTaskFunc
	statusHandler   func(ctx context.Context) (string, error)
}

// NewManager creates a plugin manager. Call SeedCorePlugins and LoadAll after construction.
func NewManager(
	plugins store.PluginRepo,
	rules store.NotificationRuleRepo,
	settings store.SystemSettingsRepo,
	agents store.AgentRepo,
	projects store.ProjectRepo,
	opts ManagerOpts,
) *Manager {
	return &Manager{
		plugins:   plugins,
		rules:     rules,
		settings:  settings,
		agents:    agents,
		projects:  projects,
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
			ID:      "core-telegram",
			Name:    "Telegram",
			Type:    model.PluginTypeNotifier,
			Kind:    "telegram",
			IsCore:  true,
			Enabled: false,
			Config:  `{}`,
		},
		{
			ID:      "core-webhook",
			Name:    "Webhook",
			Type:    model.PluginTypeNotifier,
			Kind:    "webhook",
			IsCore:  true,
			Enabled: false,
			Config:  `{}`,
		},
		{
			ID:      "core-hindsight",
			Name:    "Hindsight Memory",
			Type:    model.PluginTypeMemory,
			Kind:    "hindsight",
			IsCore:  true,
			Enabled: false,
			Config:  `{"base_url":"http://localhost:8888","api_key":""}`,
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
			slog.Info("plugin: seeded core plugin", "name", cp.Name, "kind", cp.Kind)
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
	slog.Info("plugin: loaded master switches", "core_enabled", settings.CorePluginsEnabled, "community_enabled", settings.CommunityPluginsEnabled)
	m.refreshMemoryClient(ctx)
	return nil
}

// RefreshSettings reloads the master switch state. Called after settings are updated.
func (m *Manager) RefreshSettings(ctx context.Context) error {
	return m.LoadAll(ctx)
}

// MemoryClient returns the active Hindsight memory client, or nil if the
// plugin is disabled, not configured, or the --no-plugins flag is set.
// Callers must treat nil as "memory disabled" and skip all memory operations.
func (m *Manager) MemoryClient() memory.MemoryClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memClient
}

// refreshMemoryClient rebuilds the cached memory client from the DB.
// Call after enable/disable/update of the hindsight plugin.
func (m *Manager) refreshMemoryClient(ctx context.Context) {
	if m.noPlugins {
		m.mu.Lock()
		m.memClient = nil
		m.mu.Unlock()
		return
	}

	p, err := m.plugins.Get(ctx, "core-hindsight")
	if err != nil || p == nil || !m.isPluginActive(p) {
		m.mu.Lock()
		m.memClient = nil
		m.mu.Unlock()
		return
	}

	client, err := hindsight.New(p.Config)
	if err != nil {
		slog.Error("plugin: memory: build hindsight client", "error", err)
		m.mu.Lock()
		m.memClient = nil
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	m.memClient = client
	m.mu.Unlock()
	slog.Info("plugin: memory: hindsight client loaded", "base_url", p.Config)
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
		slog.Error("plugin: unmarshal status payload", "error", err)
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
		slog.Error("plugin: list rules", "event_type", eventType, "error", err)
		return
	}

	for _, rule := range rules {
		// Project filter: nil = all projects.
		if rule.ProjectID != nil && *rule.ProjectID != projectID {
			continue
		}

		plugin, err := m.plugins.Get(ctx, rule.PluginID)
		if err != nil || plugin == nil {
			slog.Error("plugin: get plugin", "plugin_id", rule.PluginID, "error", err)
			continue
		}

		if !m.isPluginActive(plugin) {
			continue
		}

		notifier := notifiers.Get(plugin.Kind)
		if notifier == nil {
			slog.Warn("plugin: no notifier registered for kind", "kind", plugin.Kind)
			continue
		}

		// Build the notification message.
		agentName := agentID
		if m.agents != nil {
			if a, err := m.agents.Get(ctx, agentID); err == nil && a != nil {
				agentName = a.Name
			}
		}
		projectName := projectID
		if m.projects != nil {
			if p, err := m.projects.Get(ctx, projectID); err == nil && p != nil {
				projectName = p.Name
			}
		}
		msg := notifiers.NotifyMessage{
			EventType:   string(eventType),
			TaskID:      taskID,
			TaskTitle:   taskTitle,
			AgentName:   agentName,
			ProjectName: projectName,
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
				slog.Error("plugin: send failed", "kind", p.Kind, "error", err)
			} else {
				slog.Info("plugin: notification sent", "kind", p.Kind, "event_type", m.EventType)
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
		slog.Error("plugin: parse template", "error", err)
		return fmt.Sprintf("%s: %s", msg.EventType, msg.TaskTitle)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, msg); err != nil {
		slog.Error("plugin: execute template", "error", err)
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

// SetStatusHandler registers the callback used to answer /status queries from Telegram.
// Must be called before StartPollers.
func (m *Manager) SetStatusHandler(fn func(ctx context.Context) (string, error)) {
	m.mu.Lock()
	m.statusHandler = fn
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
		slog.Error("plugin: StartPollers: list plugins", "error", err)
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
		slog.Error("plugin: telegram startPoller: unmarshal config", "plugin_id", p.ID, "error", err)
		return
	}
	if !cfg.InboundEnabled {
		return
	}
	if cfg.DefaultProjectID == "" || cfg.DefaultAgentID == "" {
		slog.Warn("plugin: telegram inbound: default_project_id or default_agent_id not set, skipping", "plugin_id", p.ID)
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

	m.mu.RLock()
	statusFn := m.statusHandler
	m.mu.RUnlock()

	go func() {
		slog.Info("plugin: telegram poller starting", "plugin_id", pluginID)
		telegram.StartPoller(pollerCtx, cfg, handler, statusFn)
		slog.Info("plugin: telegram poller stopped", "plugin_id", pluginID)
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

// NotifyPluginUpdated is called by API handlers after a plugin is enabled,
// disabled, or updated. It refreshes any cached state derived from that plugin.
func (m *Manager) NotifyPluginUpdated(ctx context.Context, pluginID string) {
	if pluginID == "core-hindsight" {
		m.refreshMemoryClient(ctx)
	}
}

// TestPlugin sends a test notification through the given plugin,
// or pings the memory backend for memory plugins.
func (m *Manager) TestPlugin(ctx context.Context, pluginID string) error {
	plugin, err := m.plugins.Get(ctx, pluginID)
	if err != nil {
		return fmt.Errorf("get plugin: %w", err)
	}
	if plugin == nil {
		return fmt.Errorf("plugin not found")
	}

	// Memory plugins: ping the backend.
	if plugin.Type == model.PluginTypeMemory && plugin.Kind == "hindsight" {
		client, err := hindsight.New(plugin.Config)
		if err != nil {
			return fmt.Errorf("hindsight config error: %w", err)
		}
		if err := client.Ping(ctx); err != nil {
			return fmt.Errorf("hindsight unreachable: %w", err)
		}
		return nil
	}

	notifier := notifiers.Get(plugin.Kind)
	if notifier == nil {
		return fmt.Errorf("no notifier registered for kind %q", plugin.Kind)
	}

	msg := notifier.TestMessage()
	return notifier.Send(ctx, json.RawMessage(plugin.Config), msg)
}
