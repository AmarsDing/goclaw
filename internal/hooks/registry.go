package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Hook is a registered handler that fires at a specific pipeline lifecycle event.
type Hook struct {
	ID       string      `json:"id"`
	Event    Event       `json:"event"`
	Mode     Mode        `json:"mode"`
	Priority int         `json:"priority"` // higher = runs first
	AgentID  string      `json:"agent_id"` // "*" for global
	Matcher  Matcher     `json:"matcher,omitempty"`
	Handler  HandlerSpec `json:"handler"`
	Enabled  bool        `json:"enabled"`
}

// Matcher optionally restricts when a hook fires.
type Matcher struct {
	ToolName string `json:"tool_name,omitempty"` // only fire for this tool (pre/post_tool_use)
	Channel  string `json:"channel,omitempty"`   // only fire for this channel
	RunKind  string `json:"run_kind,omitempty"`  // "chat", "cron", "delegation", etc.
}

// HandlerSpec describes how to execute the hook.
type HandlerSpec struct {
	Type    HandlerType `json:"type"`
	Target  string      `json:"target"`            // URL for webhook, command for shell, function name for internal
	Timeout int         `json:"timeout,omitempty"` // seconds, default 30
}

// HandlerType identifies the hook execution mechanism.
type HandlerType string

const (
	HandlerWebhook  HandlerType = "webhook"  // HTTP POST to URL
	HandlerCommand  HandlerType = "command"  // shell command
	HandlerInternal HandlerType = "internal" // Go function (registered by name)
)

// Registry manages hook registration and firing.
type Registry struct {
	mu             sync.RWMutex
	hooks          map[Event][]*Hook
	internal       map[string]InternalHandler // name → handler
	executor       *Executor
	pendingEvents  []Payload
	maxPending     int
	eventCh        chan EventRecord
	rewakeCallback RewakeCallback
}

// InternalHandler is a Go function hook handler.
type InternalHandler func(ctx context.Context, payload Payload) (*Result, error)

// RewakeCallback is invoked when an async hook finishes and wants the runtime
// to re-enter the main loop or resume waiting work.
type RewakeCallback func(ctx context.Context, payload Payload, result Result)

// NewRegistry creates a hook registry with an executor.
func NewRegistry() *Registry {
	return &Registry{
		hooks:      make(map[Event][]*Hook),
		internal:   make(map[string]InternalHandler),
		executor:   NewExecutor(),
		maxPending: 128,
		eventCh:    make(chan EventRecord, 256),
	}
}

// Register adds a hook to the registry.
func (r *Registry) Register(hook Hook) error {
	if hook.ID == "" {
		return fmt.Errorf("hook ID is required")
	}
	if hook.Event == "" {
		return fmt.Errorf("hook event is required")
	}
	if hook.Mode == "" {
		hook.Mode = ModeAsync
	}

	r.mu.Lock()
	// Check for duplicate ID.
	for _, hooks := range r.hooks {
		for _, h := range hooks {
			if h.ID == hook.ID {
				r.mu.Unlock()
				return fmt.Errorf("hook %s already registered", hook.ID)
			}
		}
	}

	r.hooks[hook.Event] = append(r.hooks[hook.Event], &hook)
	replay := r.popPendingLocked(hook.Event)
	r.mu.Unlock()

	slog.Debug("hook registered", "id", hook.ID, "event", hook.Event, "mode", hook.Mode)
	if len(replay) > 0 {
		go r.replayPending(replay)
	}
	return nil
}

// Unregister removes a hook by ID.
func (r *Registry) Unregister(hookID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for event, hooks := range r.hooks {
		for i, h := range hooks {
			if h.ID == hookID {
				r.hooks[event] = append(hooks[:i], hooks[i+1:]...)
				return true
			}
		}
	}
	return false
}

// RegisterInternal registers a Go function as a named internal handler.
func (r *Registry) RegisterInternal(name string, handler InternalHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.internal[name] = handler
}

// SetRewakeCallback registers a callback for async hook completion.
func (r *Registry) SetRewakeCallback(fn RewakeCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rewakeCallback = fn
}

// Events returns a stream of structured hook execution records.
func (r *Registry) Events() <-chan EventRecord {
	return r.eventCh
}

// Fire triggers all hooks registered for the given event.
// Sync hooks run sequentially (highest priority first); async hooks run concurrently.
func (r *Registry) Fire(ctx context.Context, payload Payload) ([]Result, error) {
	if payload.Timestamp.IsZero() {
		payload.Timestamp = time.Now()
	}

	r.mu.RLock()
	hooks := r.matchingHooks(payload)
	internalHandlers := make(map[string]InternalHandler, len(r.internal))
	for k, v := range r.internal {
		internalHandlers[k] = v
	}
	rewake := r.rewakeCallback
	r.mu.RUnlock()

	if len(hooks) == 0 {
		r.bufferPending(payload)
		return nil, nil
	}

	// Separate sync and async hooks.
	var syncHooks, asyncHooks []*Hook
	for _, h := range hooks {
		if h.Mode == ModeSync {
			syncHooks = append(syncHooks, h)
		} else {
			asyncHooks = append(asyncHooks, h)
		}
	}

	var results []Result

	// Execute sync hooks sequentially.
	for _, h := range syncHooks {
		r.emitEventRecord(EventRecord{
			Status:    EventRecordStarted,
			HookID:    h.ID,
			Event:     payload.Event,
			RunID:     payload.RunID,
			AgentID:   payload.AgentID,
			Timestamp: time.Now(),
		})
		result, err := r.executeHook(ctx, h, payload, internalHandlers)
		if err != nil {
			slog.Warn("sync hook error", "hook", h.ID, "event", h.Event, "err", err)
			failure := Result{
				HookID:  h.ID,
				Success: false,
				Error:   err.Error(),
			}
			results = append(results, failure)
			r.emitEventRecord(EventRecord{
				Status:    EventRecordResponse,
				HookID:    h.ID,
				Event:     payload.Event,
				RunID:     payload.RunID,
				AgentID:   payload.AgentID,
				Timestamp: time.Now(),
				Result:    &failure,
				Error:     err.Error(),
			})
			continue
		}
		results = append(results, *result)
		r.emitEventRecord(EventRecord{
			Status:    EventRecordResponse,
			HookID:    h.ID,
			Event:     payload.Event,
			RunID:     payload.RunID,
			AgentID:   payload.AgentID,
			Timestamp: time.Now(),
			Result:    result,
		})
	}

	// Fire async hooks concurrently without blocking the pipeline. Results are
	// emitted on the event stream and may rewake the runtime.
	if len(asyncHooks) > 0 {
		for _, h := range asyncHooks {
			r.emitEventRecord(EventRecord{
				Status:    EventRecordStarted,
				HookID:    h.ID,
				Event:     payload.Event,
				RunID:     payload.RunID,
				AgentID:   payload.AgentID,
				Timestamp: time.Now(),
			})
			go func(hook *Hook) {
				result, err := r.executeHook(ctx, hook, payload, internalHandlers)
				if err != nil {
					slog.Warn("async hook error", "hook", hook.ID, "err", err)
					failure := Result{
						HookID:  hook.ID,
						Success: false,
						Error:   err.Error(),
					}
					r.emitEventRecord(EventRecord{
						Status:    EventRecordResponse,
						HookID:    hook.ID,
						Event:     payload.Event,
						RunID:     payload.RunID,
						AgentID:   payload.AgentID,
						Timestamp: time.Now(),
						Result:    &failure,
						Error:     err.Error(),
					})
					if rewake != nil && failure.Rewake {
						rewake(context.Background(), payload, failure)
					}
					return
				}
				r.emitEventRecord(EventRecord{
					Status:    EventRecordResponse,
					HookID:    hook.ID,
					Event:     payload.Event,
					RunID:     payload.RunID,
					AgentID:   payload.AgentID,
					Timestamp: time.Now(),
					Result:    result,
				})
				if rewake != nil && result.Rewake {
					rewake(context.Background(), payload, *result)
				}
			}(h)
		}
	}

	return results, nil
}

// List returns all registered hooks.
func (r *Registry) List() []Hook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []Hook
	for _, hooks := range r.hooks {
		for _, h := range hooks {
			out = append(out, *h)
		}
	}
	return out
}

func (r *Registry) matchingHooks(payload Payload) []*Hook {
	hooks := r.hooks[payload.Event]
	var matched []*Hook

	for _, h := range hooks {
		if !h.Enabled {
			continue
		}
		if h.AgentID != "*" && h.AgentID != "" && h.AgentID != payload.AgentID {
			continue
		}
		if h.Matcher.ToolName != "" {
			if tn, ok := payload.Data["tool_name"].(string); ok && tn != h.Matcher.ToolName {
				continue
			}
		}
		if h.Matcher.Channel != "" {
			if ch := payload.Channel(); ch != "" && ch != h.Matcher.Channel {
				continue
			}
		}
		if h.Matcher.RunKind != "" {
			if rk := payload.RunKind(); rk != "" && rk != h.Matcher.RunKind {
				continue
			}
		}
		matched = append(matched, h)
	}

	// Sort by priority (highest first) — simple insertion sort for small N.
	for i := 1; i < len(matched); i++ {
		for j := i; j > 0 && matched[j].Priority > matched[j-1].Priority; j-- {
			matched[j], matched[j-1] = matched[j-1], matched[j]
		}
	}

	return matched
}

func (r *Registry) executeHook(
	ctx context.Context,
	hook *Hook,
	payload Payload,
	internalHandlers map[string]InternalHandler,
) (*Result, error) {
	timeout := time.Duration(hook.Handler.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	switch hook.Handler.Type {
	case HandlerInternal:
		handler, ok := internalHandlers[hook.Handler.Target]
		if !ok {
			return nil, fmt.Errorf("internal handler %q not found", hook.Handler.Target)
		}
		result, err := handler(ctx, payload)
		if err != nil {
			return nil, err
		}
		if result == nil {
			result = &Result{Success: true}
		}
		result.HookID = hook.ID
		result.Duration = time.Since(start)
		return result, nil

	case HandlerWebhook:
		return r.executor.ExecuteWebhook(ctx, hook, payload)

	case HandlerCommand:
		return r.executor.ExecuteCommand(ctx, hook, payload)

	default:
		return nil, fmt.Errorf("unsupported handler type: %s", hook.Handler.Type)
	}
}

func (r *Registry) bufferPending(payload Payload) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingEvents = append(r.pendingEvents, payload)
	if len(r.pendingEvents) > r.maxPending {
		r.pendingEvents = r.pendingEvents[len(r.pendingEvents)-r.maxPending:]
	}
}

func (r *Registry) popPendingLocked(event Event) []Payload {
	if len(r.pendingEvents) == 0 {
		return nil
	}
	var kept []Payload
	var replay []Payload
	for _, payload := range r.pendingEvents {
		if payload.Event == event {
			replay = append(replay, payload)
			continue
		}
		kept = append(kept, payload)
	}
	r.pendingEvents = kept
	return replay
}

func (r *Registry) replayPending(payloads []Payload) {
	for _, payload := range payloads {
		_, err := r.Fire(context.Background(), payload)
		if err != nil {
			slog.Warn("replay pending hook event failed", "event", payload.Event, "err", err)
		}
	}
}

func (r *Registry) emitEventRecord(event EventRecord) {
	select {
	case r.eventCh <- event:
	default:
		slog.Debug("hook event channel full", "hook", event.HookID, "status", event.Status)
	}
}
