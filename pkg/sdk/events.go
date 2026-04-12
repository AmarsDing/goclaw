// Package sdk provides a Go SDK for embedding and controlling goclaw agents.
//
// Claude Code's StructuredIO protocol enables headless/SDK usage where external
// systems control agent runs and subscribe to structured events. This SDK provides
// the event type definitions and bridge protocol for IDE plugins, web dashboards,
// mobile apps, and other integrations.
package sdk

import "time"

// EventType identifies a structured event from the agent runtime.
type EventType string

const (
	// Lifecycle events
	EventRunStarted   EventType = "run.started"
	EventRunCompleted EventType = "run.completed"
	EventRunFailed    EventType = "run.failed"
	EventStateChange  EventType = "state.change"

	// Content events
	EventAssistantMessage EventType = "assistant.message"
	EventAssistantDelta   EventType = "assistant.delta" // streaming token
	EventThinkingDelta    EventType = "thinking.delta"  // streaming thinking

	// Tool events
	EventToolCallStart  EventType = "tool.call.start"
	EventToolCallEnd    EventType = "tool.call.end"
	EventToolCallOutput EventType = "tool.call.output"

	// Permission events
	EventPermissionRequest EventType = "permission.request"
	EventPermissionResult  EventType = "permission.result"

	// Progress events
	EventProgress EventType = "progress"

	// Session events
	EventSessionCreated  EventType = "session.created"
	EventSessionResumed  EventType = "session.resumed"
	EventSessionCompacted EventType = "session.compacted"
)

// Event is the wire format for all SDK events.
type Event struct {
	Type      EventType      `json:"type"`
	RunID     string         `json:"run_id"`
	AgentID   string         `json:"agent_id"`
	SessionKey string        `json:"session_key"`
	Timestamp time.Time      `json:"timestamp"`
	Sequence  int64          `json:"sequence"` // monotonically increasing per run
	Data      any            `json:"data"`
}

// RunStartedData accompanies EventRunStarted.
type RunStartedData struct {
	UserMessage string `json:"user_message"`
	Model       string `json:"model"`
	Provider    string `json:"provider"`
}

// RunCompletedData accompanies EventRunCompleted.
type RunCompletedData struct {
	Content    string `json:"content"`
	Iterations int    `json:"iterations"`
	ToolCalls  int    `json:"tool_calls"`
	TokensUsed int    `json:"tokens_used"`
	Duration   string `json:"duration"`
}

// RunFailedData accompanies EventRunFailed.
type RunFailedData struct {
	Error  string `json:"error"`
	State  string `json:"state"`
}

// StateChangeData accompanies EventStateChange.
type StateChangeData struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}

// ToolCallStartData accompanies EventToolCallStart.
type ToolCallStartData struct {
	ToolCallID string         `json:"tool_call_id"`
	ToolName   string         `json:"tool_name"`
	Arguments  map[string]any `json:"arguments"`
}

// ToolCallEndData accompanies EventToolCallEnd.
type ToolCallEndData struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Success    bool   `json:"success"`
	Duration   string `json:"duration"`
	Output     string `json:"output,omitempty"` // truncated for SDK
}

// PermissionRequestData accompanies EventPermissionRequest.
type PermissionRequestData struct {
	RequestID  string         `json:"request_id"`
	ToolName   string         `json:"tool_name"`
	Arguments  map[string]any `json:"arguments"`
	Risk       string         `json:"risk"`
	Class      string         `json:"class"`
}

// PermissionResultData accompanies EventPermissionResult.
type PermissionResultData struct {
	RequestID string `json:"request_id"`
	Allowed   bool   `json:"allowed"`
	Reason    string `json:"reason"`
}

// ProgressData accompanies EventProgress.
type ProgressData struct {
	Phase      string  `json:"phase"`
	Iteration  int     `json:"iteration"`
	Percentage float64 `json:"percentage,omitempty"`
	Message    string  `json:"message,omitempty"`
}

// DeltaData accompanies EventAssistantDelta and EventThinkingDelta.
type DeltaData struct {
	Content string `json:"content"`
}
