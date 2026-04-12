package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
)

// TransitionCallback is invoked on each state transition.
type TransitionCallback func(t Transition)

// Manager tracks lifecycle state for active pipeline runs and emits domain events.
type Manager struct {
	mu        sync.RWMutex
	runs      map[string]*runRecord
	callbacks []TransitionCallback
	eventBus  eventbus.DomainEventBus
}

type runRecord struct {
	current   State
	history   []Transition
	agentID   string
	tenantID  string
	userID    string
}

// NewManager creates a lifecycle manager. eventBus may be nil to disable event emission.
func NewManager(eb eventbus.DomainEventBus) *Manager {
	return &Manager{
		runs:     make(map[string]*runRecord),
		eventBus: eb,
	}
}

// Register initialises lifecycle tracking for a new run.
func (m *Manager) Register(runID, agentID, tenantID, userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[runID] = &runRecord{
		current:  StateIdle,
		agentID:  agentID,
		tenantID: tenantID,
		userID:   userID,
	}
}

// Transition moves a run to a new state. Returns an error for invalid transitions.
func (m *Manager) Transition(ctx context.Context, runID string, to State, reason string) error {
	m.mu.Lock()
	rec, ok := m.runs[runID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("lifecycle: unknown run %s", runID)
	}
	from := rec.current

	if !CanTransition(from, to) {
		m.mu.Unlock()
		return fmt.Errorf("lifecycle: invalid transition %s → %s for run %s", from, to, runID)
	}

	t := Transition{
		From:      from,
		To:        to,
		RunID:     runID,
		Timestamp: time.Now(),
		Reason:    reason,
	}
	rec.current = to
	rec.history = append(rec.history, t)

	// Snapshot callback list under lock, release before invoking.
	cbs := make([]TransitionCallback, len(m.callbacks))
	copy(cbs, m.callbacks)
	agentID := rec.agentID
	tenantID := rec.tenantID
	userID := rec.userID
	m.mu.Unlock()

	for _, cb := range cbs {
		cb(t)
	}

	if m.eventBus != nil {
		m.eventBus.Publish(eventbus.DomainEvent{
			Type:      EventLifecycleTransition,
			SourceID:  runID,
			AgentID:   agentID,
			TenantID:  tenantID,
			UserID:    userID,
			Timestamp: t.Timestamp,
			Payload: LifecycleTransitionPayload{
				RunID:  runID,
				From:   from.String(),
				To:     to.String(),
				Reason: reason,
			},
		})
	}

	slog.Debug("lifecycle transition",
		"run", runID, "from", from, "to", to, "reason", reason)

	return nil
}

// Current returns the current state of a run.
func (m *Manager) Current(runID string) (State, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.runs[runID]
	if !ok {
		return StateIdle, false
	}
	return rec.current, true
}

// History returns the full transition history for a run.
func (m *Manager) History(runID string) []Transition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.runs[runID]
	if !ok {
		return nil
	}
	out := make([]Transition, len(rec.history))
	copy(out, rec.history)
	return out
}

// OnTransition registers a callback invoked on every state transition.
func (m *Manager) OnTransition(fn TransitionCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, fn)
}

// Cleanup removes tracking for a run (call after terminal state).
func (m *Manager) Cleanup(runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runs, runID)
}

// EventLifecycleTransition is the event type for lifecycle changes.
const EventLifecycleTransition eventbus.EventType = "lifecycle.transition"

// LifecycleTransitionPayload is emitted on each state transition.
type LifecycleTransitionPayload struct {
	RunID  string `json:"run_id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}
