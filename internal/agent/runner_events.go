package agent

import (
	"errors"

	"github.com/solarisjon/phoenix/internal/model"
)

// ErrTaskCancelledByUser is the cancel cause set when a task is stopped via the API.
var ErrTaskCancelledByUser = errors.New("task cancelled by user")

// BudgetInfo carries the budget state when a project's cost limit is exceeded.
// It is set on a StreamEvent to trigger a budget.exceeded WebSocket broadcast.
type BudgetInfo struct {
	ProjectID string
	SpentUSD  float64
	BudgetUSD float64
	Period    string
}

// StreamEvent is emitted during task execution and consumed by the WebSocket hub.
type StreamEvent struct {
	TaskID  string
	AgentID string
	// One of the following is set per event:
	Chunk          *string // partial output text
	StatusDone     *model.TaskStatus
	Err            error
	BudgetExceeded *BudgetInfo // non-nil when the project budget was exceeded
}

// EventHandler is called with each StreamEvent during task execution.
// Implementations must be non-blocking (e.g. send to a buffered channel).
type EventHandler func(StreamEvent)

// emit calls the event handler if one is registered.
func (r *Runner) emit(ev StreamEvent) {
	if r.onEvent != nil {
		r.onEvent(ev)
	}
}
