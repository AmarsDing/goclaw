// Package hooks provides a standardised lifecycle hook system for the agent pipeline.
//
// Claude Code's insight: external extensions must follow a protocol, not just callbacks.
// Hooks are fired at well-defined pipeline points, support sync and async modes,
// and integrate with the DomainEventBus for observability.
package hooks

import "time"

// Event identifies a pipeline lifecycle point where hooks can be attached.
type Event string

const (
	// Pipeline lifecycle
	EventSessionStart Event = "session.start"
	EventSessionEnd   Event = "session.end"

	// Think stage
	EventPreThink  Event = "pre_think"
	EventPostThink Event = "post_think"

	// Tool execution
	EventPreToolUse  Event = "pre_tool_use"
	EventPostToolUse Event = "post_tool_use"

	// Permission evaluation
	EventPrePermission  Event = "pre_permission"
	EventPostPermission Event = "post_permission"

	// Context management
	EventOnCompact Event = "on_compact"
	EventOnResume  Event = "on_resume"

	// Error handling
	EventOnError Event = "on_error"

	// Lifecycle state changes
	EventLifecycleChange Event = "lifecycle.change"
)

// AllEvents is the complete set of hook events.
var AllEvents = []Event{
	EventSessionStart, EventSessionEnd,
	EventPreThink, EventPostThink,
	EventPreToolUse, EventPostToolUse,
	EventPrePermission, EventPostPermission,
	EventOnCompact, EventOnResume,
	EventOnError, EventLifecycleChange,
}

// Mode controls how a hook is executed relative to the pipeline.
type Mode string

const (
	ModeSync  Mode = "sync"  // blocks pipeline until hook completes
	ModeAsync Mode = "async" // fire-and-forget, does not block
)

// Payload carries event-specific data to hook handlers.
type Payload struct {
	Event      Event          `json:"event"`
	RunID      string         `json:"run_id"`
	AgentID    string         `json:"agent_id"`
	SessionKey string         `json:"session_key"`
	Timestamp  time.Time      `json:"timestamp"`
	Data       map[string]any `json:"data,omitempty"`
}

// Result is the response from a hook execution.
type Result struct {
	HookID       string         `json:"hook_id"`
	Success      bool           `json:"success"`
	Action       ResultAction   `json:"action,omitempty"`
	Output       string         `json:"output,omitempty"`
	Error        string         `json:"error,omitempty"`
	Duration     time.Duration  `json:"duration"`
	Mutations    map[string]any `json:"mutations,omitempty"`
	Rewake       bool           `json:"rewake,omitempty"`
	RewakeReason string         `json:"rewake_reason,omitempty"`
}

// ResultAction tells the runtime how a synchronous hook wants to affect execution.
type ResultAction string

const (
	ResultActionContinue ResultAction = "continue"
	ResultActionBlock    ResultAction = "block"
	ResultActionModify   ResultAction = "modify"
)

// EventRecord captures structured hook execution telemetry.
type EventRecord struct {
	Status    EventRecordStatus `json:"status"`
	HookID    string            `json:"hook_id"`
	Event     Event             `json:"event"`
	RunID     string            `json:"run_id"`
	AgentID   string            `json:"agent_id"`
	Timestamp time.Time         `json:"timestamp"`
	Result    *Result           `json:"result,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// EventRecordStatus identifies a hook execution lifecycle update.
type EventRecordStatus string

const (
	EventRecordStarted  EventRecordStatus = "started"
	EventRecordProgress EventRecordStatus = "progress"
	EventRecordResponse EventRecordStatus = "response"
)

// SessionPayload is the typed body for session lifecycle hooks.
type SessionPayload struct {
	Channel string `json:"channel,omitempty"`
	RunKind string `json:"run_kind,omitempty"`
	UserID  string `json:"user_id,omitempty"`
}

// ThinkPayload is the typed body for think-stage hooks.
type ThinkPayload struct {
	Iteration int    `json:"iteration"`
	Model     string `json:"model,omitempty"`
	Provider  string `json:"provider,omitempty"`
}

// ToolPayload is the typed body for tool execution hooks.
type ToolPayload struct {
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolName   string         `json:"tool_name"`
	Arguments  map[string]any `json:"arguments,omitempty"`
	Channel    string         `json:"channel,omitempty"`
	RunKind    string         `json:"run_kind,omitempty"`
}

// PermissionPayload is the typed body for permission hooks.
type PermissionPayload struct {
	RequestID string         `json:"request_id,omitempty"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Action    string         `json:"action,omitempty"`
	Reason    string         `json:"reason,omitempty"`
}

// CompactPayload is the typed body for compaction hooks.
type CompactPayload struct {
	Level        string `json:"level,omitempty"`
	TokensBefore int    `json:"tokens_before,omitempty"`
	TokensAfter  int    `json:"tokens_after,omitempty"`
}

// ResumePayload is the typed body for resume hooks.
type ResumePayload struct {
	InterruptKind string `json:"interrupt_kind,omitempty"`
	Repaired      bool   `json:"repaired,omitempty"`
	RepairCount   int    `json:"repair_count,omitempty"`
}

// ErrorPayload is the typed body for error hooks.
type ErrorPayload struct {
	Stage string `json:"stage,omitempty"`
	Error string `json:"error"`
}

// LifecyclePayload is the typed body for lifecycle transition hooks.
type LifecyclePayload struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}

func (p Payload) ToolName() string {
	if p.Data == nil {
		return ""
	}
	if toolName, ok := p.Data["tool_name"].(string); ok {
		return toolName
	}
	return ""
}

func (p Payload) Channel() string {
	if p.Data == nil {
		return ""
	}
	if channel, ok := p.Data["channel"].(string); ok {
		return channel
	}
	return ""
}

func (p Payload) RunKind() string {
	if p.Data == nil {
		return ""
	}
	if runKind, ok := p.Data["run_kind"].(string); ok {
		return runKind
	}
	return ""
}

func (p SessionPayload) AsMap() map[string]any {
	return map[string]any{
		"channel":  p.Channel,
		"run_kind": p.RunKind,
		"user_id":  p.UserID,
	}
}

func (p ThinkPayload) AsMap() map[string]any {
	return map[string]any{
		"iteration": p.Iteration,
		"model":     p.Model,
		"provider":  p.Provider,
	}
}

func (p ToolPayload) AsMap() map[string]any {
	return map[string]any{
		"tool_call_id": p.ToolCallID,
		"tool_name":    p.ToolName,
		"arguments":    p.Arguments,
		"channel":      p.Channel,
		"run_kind":     p.RunKind,
	}
}

func (p PermissionPayload) AsMap() map[string]any {
	return map[string]any{
		"request_id": p.RequestID,
		"tool_name":  p.ToolName,
		"arguments":  p.Arguments,
		"action":     p.Action,
		"reason":     p.Reason,
	}
}

func (p CompactPayload) AsMap() map[string]any {
	return map[string]any{
		"level":         p.Level,
		"tokens_before": p.TokensBefore,
		"tokens_after":  p.TokensAfter,
	}
}

func (p ResumePayload) AsMap() map[string]any {
	return map[string]any{
		"interrupt_kind": p.InterruptKind,
		"repaired":       p.Repaired,
		"repair_count":   p.RepairCount,
	}
}

func (p ErrorPayload) AsMap() map[string]any {
	return map[string]any{
		"stage": p.Stage,
		"error": p.Error,
	}
}

func (p LifecyclePayload) AsMap() map[string]any {
	return map[string]any{
		"from":   p.From,
		"to":     p.To,
		"reason": p.Reason,
	}
}
