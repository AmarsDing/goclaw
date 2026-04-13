package sdk

import (
	"encoding/json"
	"fmt"
)

// GatewayEventFrame is the JSON shape emitted on GET /v1/bridge/events (SSE data lines).
// It aligns with pkg/protocol.EventFrame for client-side parsing.
type GatewayEventFrame struct {
	Type    string          `json:"type"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Seq     int64           `json:"seq,omitempty"`
	RunID   string          `json:"run_id,omitempty"`
}

// ParseGatewayEventFrame unmarshals one SSE JSON line into GatewayEventFrame.
func ParseGatewayEventFrame(raw json.RawMessage) (*GatewayEventFrame, error) {
	var f GatewayEventFrame
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse gateway frame: %w", err)
	}
	return &f, nil
}

// ToSDKEvent maps a gateway event name + payload into a typed sdk.Event when possible.
// Unknown event names still return an Event with Type set from the gateway "event" field.
func ToSDKEvent(runID, agentID, sessionKey string, seq int64, frame *GatewayEventFrame) Event {
	ev := Event{
		RunID:      runID,
		AgentID:    agentID,
		SessionKey: sessionKey,
		Sequence:   seq,
		Data:       json.RawMessage(frame.Payload),
	}
	if frame.RunID != "" {
		ev.RunID = frame.RunID
	}
	switch EventType(frame.Event) {
	case EventPermissionRequest:
		ev.Type = EventPermissionRequest
	case EventPermissionResult:
		ev.Type = EventPermissionResult
	case EventPermissionModeChange:
		ev.Type = EventPermissionModeChange
	case EventStateChange:
		ev.Type = EventStateChange
	case EventRunStarted:
		ev.Type = EventRunStarted
	case EventRunCompleted:
		ev.Type = EventRunCompleted
	case EventRunFailed:
		ev.Type = EventRunFailed
	case EventToolCallStart:
		ev.Type = EventToolCallStart
	case EventToolCallEnd:
		ev.Type = EventToolCallEnd
	default:
		ev.Type = EventType(frame.Event)
	}
	return ev
}
