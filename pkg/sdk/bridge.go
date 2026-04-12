package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Command represents an instruction from the SDK client to the runtime.
type Command struct {
	Type    CommandType     `json:"type"`
	RunID   string          `json:"run_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// CommandType identifies what the client wants to do.
type CommandType string

const (
	CmdStartRun    CommandType = "start_run"
	CmdAbortRun    CommandType = "abort_run"
	CmdPauseRun    CommandType = "pause_run"
	CmdResumeRun   CommandType = "resume_run"
	CmdSendMessage CommandType = "send_message"
	CmdApprove     CommandType = "approve"
	CmdDeny        CommandType = "deny"
	CmdPing        CommandType = "ping"
)

// StartRunPayload is the payload for CmdStartRun.
type StartRunPayload struct {
	AgentID    string `json:"agent_id"`
	Message    string `json:"message"`
	SessionKey string `json:"session_key,omitempty"` // empty = new session
	UserID     string `json:"user_id"`
	Channel    string `json:"channel,omitempty"`
	Stream     bool   `json:"stream"`
}

// ApprovePayload is the payload for CmdApprove / CmdDeny.
type ApprovePayload struct {
	RequestID string `json:"request_id"`
	Remember  bool   `json:"remember"` // "don't ask again"
}

// EventSink receives events from the runtime.
type EventSink interface {
	Send(event Event) error
	Close() error
}

// Bridge connects an SDK client to the agent runtime.
// It translates Commands into runtime actions and routes Events back to the client.
type Bridge struct {
	mu        sync.RWMutex
	sinks     map[string]EventSink // runID → sink
	handlers  map[CommandType]CommandHandler
	globalSeq atomic.Int64
	runSeqs   map[string]*atomic.Int64
}

// CommandHandler processes a single command type.
type CommandHandler func(ctx context.Context, cmd Command) error

// NewBridge creates a bridge instance.
func NewBridge() *Bridge {
	return &Bridge{
		sinks:    make(map[string]EventSink),
		handlers: make(map[CommandType]CommandHandler),
		runSeqs:  make(map[string]*atomic.Int64),
	}
}

// OnCommand registers a handler for a command type.
func (b *Bridge) OnCommand(cmdType CommandType, handler CommandHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[cmdType] = handler
}

// HandleCommand processes an incoming command from a client.
func (b *Bridge) HandleCommand(ctx context.Context, cmd Command) error {
	b.mu.RLock()
	handler, ok := b.handlers[cmd.Type]
	b.mu.RUnlock()

	if !ok {
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
	return handler(ctx, cmd)
}

// Subscribe registers an event sink for a run. Returns an unsubscribe function.
func (b *Bridge) Subscribe(runID string, sink EventSink) func() {
	b.mu.Lock()
	b.sinks[runID] = sink
	if _, ok := b.runSeqs[runID]; !ok {
		b.runSeqs[runID] = &atomic.Int64{}
	}
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		delete(b.sinks, runID)
		if _, ok := b.sinks[runID]; !ok {
			delete(b.runSeqs, runID)
		}
		b.mu.Unlock()
	}
}

// Emit sends an event to the subscribed sink for the given run.
func (b *Bridge) Emit(event Event) {
	event.Sequence = b.nextRunSequence(event.RunID)
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	b.mu.RLock()
	sink, ok := b.sinks[event.RunID]
	b.mu.RUnlock()

	if !ok {
		return
	}

	if err := sink.Send(event); err != nil {
		b.mu.Lock()
		delete(b.sinks, event.RunID)
		b.mu.Unlock()
	}
}

// EmitAll sends an event to all active sinks (for global events).
func (b *Bridge) EmitAll(event Event) {
	event.Sequence = b.globalSeq.Add(1)
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	b.mu.RLock()
	sinks := make(map[string]EventSink, len(b.sinks))
	for k, v := range b.sinks {
		sinks[k] = v
	}
	b.mu.RUnlock()

	for _, sink := range sinks {
		_ = sink.Send(event)
	}
}

// ActiveRuns returns the number of runs with active event subscriptions.
func (b *Bridge) ActiveRuns() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.sinks)
}

func (b *Bridge) nextRunSequence(runID string) int64 {
	if runID == "" {
		return b.globalSeq.Add(1)
	}
	b.mu.RLock()
	seq := b.runSeqs[runID]
	b.mu.RUnlock()
	if seq != nil {
		return seq.Add(1)
	}

	b.mu.Lock()
	seq = b.runSeqs[runID]
	if seq == nil {
		seq = &atomic.Int64{}
		b.runSeqs[runID] = seq
	}
	b.mu.Unlock()
	return seq.Add(1)
}
