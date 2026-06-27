// Package memory defines the MemoryClient interface used by the agent runner
// to store and retrieve persistent agent memories.
package memory

import "context"

// MemoryClient is implemented by any memory backend (e.g. Hindsight).
// All methods are safe to call concurrently.
// Implementations must be tolerant: errors must never propagate to block task execution.
type MemoryClient interface {
	// Retain stores content as a new memory for the given agent.
	Retain(ctx context.Context, agentID, content string) error

	// Recall retrieves memories relevant to the given query for the agent.
	// Returns the recalled text (may be empty if no relevant memories exist).
	Recall(ctx context.Context, agentID, query string) (string, error)

	// ClearBank deletes all memories for the given agent.
	ClearBank(ctx context.Context, agentID string) error

	// Ping checks that the memory backend is reachable.
	Ping(ctx context.Context) error
}
