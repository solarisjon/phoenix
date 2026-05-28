package api

import (
	"encoding/json"
	"testing"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/model"
)

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	h := NewHub()

	ch := h.subscribe()
	if h.ClientCount() != 1 {
		t.Errorf("want 1 client, got %d", h.ClientCount())
	}

	h.unsubscribe(ch)
	if h.ClientCount() != 0 {
		t.Errorf("want 0 clients, got %d", h.ClientCount())
	}
}

func TestHub_Broadcast(t *testing.T) {
	h := NewHub()
	ch := h.subscribe()
	defer h.unsubscribe(ch)

	ev := Event{Type: EventTaskStatusChanged, Payload: map[string]string{"task_id": "t1"}}
	h.Broadcast(ev)

	select {
	case data := <-ch:
		var got Event
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Type != EventTaskStatusChanged {
			t.Errorf("type = %q, want %q", got.Type, EventTaskStatusChanged)
		}
	default:
		t.Fatal("expected message on channel")
	}
}

func TestHub_MultipleClients(t *testing.T) {
	h := NewHub()
	ch1 := h.subscribe()
	ch2 := h.subscribe()
	defer h.unsubscribe(ch1)
	defer h.unsubscribe(ch2)

	h.Broadcast(Event{Type: "test"})

	for _, ch := range []chan []byte{ch1, ch2} {
		select {
		case <-ch:
		default:
			t.Error("expected message on client channel")
		}
	}
}

func TestHub_SlowClientDropped(t *testing.T) {
	h := NewHub()
	// Create a channel with zero buffer so it's always full.
	ch := make(chan []byte, 0)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	// Should not block.
	h.Broadcast(Event{Type: "test"})
	// No assertion needed — success means it didn't deadlock.
}

func TestHub_BroadcastAgentEvent_Chunk(t *testing.T) {
	h := NewHub()
	ch := h.subscribe()
	defer h.unsubscribe(ch)

	chunk := "hello"
	h.BroadcastAgentEvent(agent.StreamEvent{
		TaskID:  "t1",
		AgentID: "a1",
		Chunk:   &chunk,
	}, nil)

	select {
	case data := <-ch:
		var ev Event
		json.Unmarshal(data, &ev)
		if ev.Type != EventTaskOutputStream {
			t.Errorf("type = %q, want %q", ev.Type, EventTaskOutputStream)
		}
	default:
		t.Fatal("expected chunk event")
	}
}

func TestHub_BroadcastAgentEvent_Completed(t *testing.T) {
	h := NewHub()
	ch := h.subscribe()
	defer h.unsubscribe(ch)

	status := model.TaskStatusCompleted
	h.BroadcastAgentEvent(agent.StreamEvent{
		TaskID:     "t1",
		AgentID:    "a1",
		StatusDone: &status,
	}, nil)

	select {
	case data := <-ch:
		var ev Event
		json.Unmarshal(data, &ev)
		if ev.Type != EventTaskStatusChanged {
			t.Errorf("type = %q, want %q", ev.Type, EventTaskStatusChanged)
		}
	default:
		t.Fatal("expected status event")
	}
}

func TestHub_BroadcastAgentEvent_AwaitingApproval(t *testing.T) {
	h := NewHub()
	ch := h.subscribe()
	defer h.unsubscribe(ch)

	status := model.TaskStatusAwaitingApproval
	h.BroadcastAgentEvent(agent.StreamEvent{
		TaskID:     "t1",
		AgentID:    "a1",
		StatusDone: &status,
	}, nil)

	// Should receive two events: status_changed + inbox.new_item
	got := map[EventType]bool{}
	for i := 0; i < 2; i++ {
		select {
		case data := <-ch:
			var ev Event
			json.Unmarshal(data, &ev)
			got[ev.Type] = true
		default:
			t.Fatalf("expected event %d", i+1)
		}
	}
	if !got[EventTaskStatusChanged] {
		t.Error("missing task.status_changed event")
	}
	if !got[EventInboxNewItem] {
		t.Error("missing inbox.new_item event")
	}
}
