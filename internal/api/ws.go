package api

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// handleWS upgrades the connection to WebSocket and streams events to the client
// until it disconnects or the server shuts down.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Allow localhost origins for local development (Vite dev server).
		// In production the embedded frontend is same-origin so no pattern is
		// needed, but listing loopback addresses explicitly is still correct.
		OriginPatterns: allowedWSOrigins(),
	})
	if err != nil {
		log.Printf("ws: accept: %v", err)
		return
	}
	defer conn.CloseNow()

	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	log.Printf("ws: client connected (%d total)", s.hub.ClientCount())
	defer log.Printf("ws: client disconnected (%d remaining)", s.hub.ClientCount()-1)

	ctx := conn.CloseRead(r.Context())

	// Send a hello ping so the client knows the connection is live.
	if err := wsjson.Write(ctx, conn, Event{Type: "connected", Payload: map[string]string{"status": "ok"}}); err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return

		case data, ok := <-ch:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// allowedWSOrigins returns the list of origin patterns permitted for WebSocket
// upgrades. Loopback addresses are always allowed for local development.
// Additional origins can be appended via PHOENIX_CORS_ORIGIN (comma-separated).
func allowedWSOrigins() []string {
	patterns := []string{"localhost:*", "127.0.0.1:*"}
	for _, e := range strings.Split(os.Getenv("PHOENIX_CORS_ORIGIN"), ",") {
		if e = strings.TrimSpace(e); e != "" {
			patterns = append(patterns, e)
		}
	}
	return patterns
}
