package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// handleBridgeSSE streams the same EventFrame JSON as WebSocket clients, over Server-Sent Events.
// Authenticated with the gateway token (see tokenAuthMiddleware). Intended for SDK / remote IDE
// integrations that prefer SSE over WebSocket.
func (s *Server) handleBridgeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeGatewayError(w, http.StatusInternalServerError, "streaming_unsupported",
			"response writer does not support streaming")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	subID := "sse:" + uuid.NewString()
	ch := make(chan bus.Event, 128)
	s.eventPub.Subscribe(subID, func(e bus.Event) {
		select {
		case ch <- e:
		default:
			// drop if consumer is slow
		}
	})
	defer s.eventPub.Unsubscribe(subID)

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case e := <-ch:
			frame := protocol.NewEvent(e.Name, e.Payload)
			data, err := json.Marshal(frame)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": ping %d\n\n", time.Now().Unix()); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			slog.Debug("bridge sse closed", "remote", r.RemoteAddr)
			return
		}
	}
}
