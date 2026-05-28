package api

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/store"
)

// Hub manages all active WebSocket client connections and broadcasts events to them.
// It is safe for concurrent use.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

// NewHub creates a ready-to-use Hub.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[chan []byte]struct{}),
	}
}

// subscribe registers a new client channel. The caller must call unsubscribe
// when the connection closes.
func (h *Hub) subscribe() chan []byte {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// unsubscribe removes the client channel and closes it.
func (h *Hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

// Broadcast serialises ev to JSON and sends it to every connected client.
// Slow clients have their event dropped rather than blocking the broadcaster.
func (h *Hub) Broadcast(ev Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		log.Printf("hub: marshal event: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.clients {
		select {
		case ch <- data:
		default:
			log.Printf("hub: client too slow, dropping event %s", ev.Type)
		}
	}
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// BroadcastAgentEvent translates an agent.StreamEvent into the appropriate
// typed Event and broadcasts it to all WebSocket clients.
// taskRepo is used to look up project context for inbox events.
func (h *Hub) BroadcastAgentEvent(ev agent.StreamEvent, tasks store.TaskRepo) {
	// Streaming chunk.
	if ev.Chunk != nil {
		h.Broadcast(Event{
			Type: EventTaskOutputStream,
			Payload: TaskStreamPayload{
				TaskID:  ev.TaskID,
				AgentID: ev.AgentID,
				Chunk:   *ev.Chunk,
			},
		})
		return
	}

	// Task finished (completed, failed, awaiting_approval).
	if ev.StatusDone != nil {
		status := *ev.StatusDone

		// Look up project ID and cost for the payload.
		var projectID string
		var costUSD float64
		var title string
		if tasks != nil {
			if t, err := tasks.Get(context.Background(), ev.TaskID); err == nil && t != nil {
				projectID = t.ProjectID
				costUSD = t.CostUSD
				title = t.Title
			}
		}

		h.Broadcast(Event{
			Type: EventTaskStatusChanged,
			Payload: TaskStatusPayload{
				TaskID:    ev.TaskID,
				AgentID:   ev.AgentID,
				ProjectID: projectID,
				Status:    status,
				CostUSD:   costUSD,
			},
		})

		// Also emit an inbox event when awaiting approval.
		if status == model.TaskStatusAwaitingApproval {
			h.Broadcast(Event{
				Type: EventInboxNewItem,
				Payload: InboxPayload{
					TaskID:    ev.TaskID,
					AgentID:   ev.AgentID,
					ProjectID: projectID,
					Title:     title,
				},
			})
		}
	}
}
