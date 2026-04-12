package lifecycle

import (
	"context"
	"testing"
)

func TestTransitionHappyPath(t *testing.T) {
	m := NewManager(nil)
	m.Register("run-1", "agent-1", "tenant-1", "user-1")

	steps := []State{
		StateInitializing,
		StateThinking,
		StateActing,
		StateObserving,
		StateFinalizing,
		StateCompleted,
	}

	for _, s := range steps {
		if err := m.Transition(context.Background(), "run-1", s, ""); err != nil {
			t.Fatalf("transition to %s: %v", s, err)
		}
	}

	cur, ok := m.Current("run-1")
	if !ok || cur != StateCompleted {
		t.Fatalf("expected completed, got %s (ok=%v)", cur, ok)
	}

	hist := m.History("run-1")
	if len(hist) != len(steps) {
		t.Fatalf("expected %d transitions, got %d", len(steps), len(hist))
	}
}

func TestInvalidTransition(t *testing.T) {
	m := NewManager(nil)
	m.Register("run-2", "a", "t", "u")

	err := m.Transition(context.Background(), "run-2", StateActing, "skip ahead")
	if err == nil {
		t.Fatal("expected error for idle→acting")
	}
}

func TestTerminalState(t *testing.T) {
	if !StateCompleted.IsTerminal() {
		t.Error("completed should be terminal")
	}
	if !StateFailed.IsTerminal() {
		t.Error("failed should be terminal")
	}
	if StateThinking.IsTerminal() {
		t.Error("thinking should not be terminal")
	}
}

func TestCallbackFired(t *testing.T) {
	m := NewManager(nil)
	m.Register("run-3", "a", "t", "u")

	var got []Transition
	m.OnTransition(func(tr Transition) {
		got = append(got, tr)
	})

	_ = m.Transition(context.Background(), "run-3", StateInitializing, "start")
	_ = m.Transition(context.Background(), "run-3", StateThinking, "")

	if len(got) != 2 {
		t.Fatalf("expected 2 callbacks, got %d", len(got))
	}
	if got[0].From != StateIdle || got[0].To != StateInitializing {
		t.Errorf("unexpected first transition: %+v", got[0])
	}
}
