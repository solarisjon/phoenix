package api

import (
	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/model"
)

// EventType identifies the kind of real-time event.
type EventType string

const (
	EventTaskStatusChanged  EventType = "task.status_changed"
	EventTaskOutputStream   EventType = "task.output_stream"
	EventAgentStatusChanged EventType = "agent.status_changed"
	EventInboxNewItem       EventType = "inbox.new_item"
	EventAgentDraftCreated  EventType = "agent_draft.created"
	EventMemoCreated        EventType = "memo.created"
	EventBudgetExceeded     EventType = "budget.exceeded"
	EventTaskPromptTrimmed  EventType = "task.prompt_trimmed"
)

// Event is the envelope sent over the WebSocket to every connected client.
type Event struct {
	Type    EventType   `json:"type"`
	Payload interface{} `json:"payload"`
}

// TaskStatusPayload is sent when a task changes status.
type TaskStatusPayload struct {
	TaskID    string           `json:"task_id"`
	AgentID   string           `json:"agent_id"`
	ProjectID string           `json:"project_id"`
	Status    model.TaskStatus `json:"status"`
	CostUSD   float64          `json:"cost_usd"`
	Title     string           `json:"title"` // task title for notification messages
}

// TaskStreamPayload is sent for each streamed output chunk.
type TaskStreamPayload struct {
	TaskID  string `json:"task_id"`
	AgentID string `json:"agent_id"`
	Chunk   string `json:"chunk"`
}

// InboxPayload is sent when a new inbox item appears.
type InboxPayload struct {
	TaskID    string `json:"task_id"`
	AgentID   string `json:"agent_id"`
	ProjectID string `json:"project_id"`
	Title     string `json:"title"`
}

// PromptTrimmedPayload is sent when a task's prompt had to be shrunk to fit
// the model's context window (local models). Trims mirror agent.Trim.
type PromptTrimmedPayload struct {
	TaskID        string       `json:"task_id"`
	AgentID       string       `json:"agent_id"`
	Model         string       `json:"model"`
	ContextWindow int          `json:"context_window"`
	Budget        int          `json:"budget"`
	PromptTokens  int          `json:"prompt_tokens"`
	Trims         []agent.Trim `json:"trims"`
}

// BudgetExceededPayload is sent when a project's cost budget is exceeded.
type BudgetExceededPayload struct {
	ProjectID string  `json:"project_id"`
	SpentUSD  float64 `json:"spent_usd"`
	BudgetUSD float64 `json:"budget_usd"`
	Period    string  `json:"period"`
}
