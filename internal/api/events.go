package api

import "github.com/solarisjon/phoenix/internal/model"

// EventType identifies the kind of real-time event.
type EventType string

const (
	EventTaskStatusChanged  EventType = "task.status_changed"
	EventTaskOutputStream   EventType = "task.output_stream"
	EventAgentStatusChanged EventType = "agent.status_changed"
	EventInboxNewItem       EventType = "inbox.new_item"
	EventAgentDraftCreated  EventType = "agent_draft.created"
	EventMemoCreated        EventType = "memo.created"
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
