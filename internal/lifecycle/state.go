// Package lifecycle provides a state machine for agent run lifecycle management.
// Each pipeline run transitions through well-defined states, with every transition
// emitted as a DomainEvent for observability, hook integration, and audit.
package lifecycle

import (
	"fmt"
	"time"
)

// State represents a discrete phase of an agent run.
type State int

const (
	StateIdle             State = iota // run not yet started
	StateInitializing                  // loading context, workspace, session history
	StateThinking                      // calling LLM
	StateActing                        // executing tool calls
	StateAwaitingApproval              // blocked on permission / user action
	StateObserving                     // processing tool results
	StateCompacting                    // compressing context (prune / compact)
	StateResuming                      // restoring interrupted session
	StateFinalizing                    // sanitizing output, flushing messages
	StateCompleted                     // run finished normally
	StateFailed                        // run aborted due to error
	StateInterrupted                   // run cancelled by user or timeout
)

// String returns the human-readable name for a lifecycle state.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateInitializing:
		return "initializing"
	case StateThinking:
		return "thinking"
	case StateActing:
		return "acting"
	case StateAwaitingApproval:
		return "awaiting_approval"
	case StateObserving:
		return "observing"
	case StateCompacting:
		return "compacting"
	case StateResuming:
		return "resuming"
	case StateFinalizing:
		return "finalizing"
	case StateCompleted:
		return "completed"
	case StateFailed:
		return "failed"
	case StateInterrupted:
		return "interrupted"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// IsTerminal reports whether the state is a final state (no further transitions).
func (s State) IsTerminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateInterrupted
}

// Transition records a single state change.
type Transition struct {
	From      State     `json:"from"`
	To        State     `json:"to"`
	RunID     string    `json:"run_id"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason,omitempty"`
}

// validTransitions defines the allowed state graph. Key = from, value = set of legal targets.
var validTransitions = map[State][]State{
	StateIdle:             {StateInitializing},
	StateInitializing:     {StateThinking, StateResuming, StateFailed},
	StateThinking:         {StateActing, StateCompacting, StateFinalizing, StateFailed, StateInterrupted},
	StateActing:           {StateAwaitingApproval, StateObserving, StateFailed, StateInterrupted},
	StateAwaitingApproval: {StateActing, StateFailed, StateInterrupted},
	StateObserving:        {StateThinking, StateFinalizing, StateFailed, StateInterrupted},
	StateCompacting:       {StateThinking, StateFinalizing, StateFailed},
	StateResuming:         {StateThinking, StateFailed},
	StateFinalizing:       {StateCompleted, StateFailed},
}

// CanTransition reports whether from→to is a valid state transition.
func CanTransition(from, to State) bool {
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}
