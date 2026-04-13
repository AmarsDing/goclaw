package sdk

import (
	"sync"
	"testing"
)

type recordingSink struct {
	mu     sync.Mutex
	events []Event
}

func (r *recordingSink) Send(e Event) error {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	return nil
}

func (r *recordingSink) Close() error { return nil }

func (r *recordingSink) sequences() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, len(r.events))
	for i := range r.events {
		out[i] = r.events[i].Sequence
	}
	return out
}

func TestBridgeEmit_SequencePerRun(t *testing.T) {
	b := NewBridge()
	sinkA := &recordingSink{}
	sinkB := &recordingSink{}

	unsubA := b.Subscribe("run-a", sinkA)
	unsubB := b.Subscribe("run-b", sinkB)
	defer unsubA()
	defer unsubB()

	b.Emit(Event{RunID: "run-a", Type: EventRunStarted})
	b.Emit(Event{RunID: "run-b", Type: EventRunStarted})
	b.Emit(Event{RunID: "run-a", Type: EventToolCallStart})

	if got := sinkA.sequences(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("run-a sequences want [1 2], got %v", got)
	}
	if got := sinkB.sequences(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("run-b sequences want [1], got %v", got)
	}
}

func TestBridgeEmitAll_UsesGlobalSequence(t *testing.T) {
	b := NewBridge()
	sink := &recordingSink{}
	defer b.Subscribe("x", sink)()

	b.EmitAll(Event{RunID: "x", Type: EventRunStarted})
	// EmitAll assigns a global monotonic sequence (shared across sinks for this broadcast).
	if got := sink.sequences(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("EmitAll want global seq 1, got %v", got)
	}
	b.EmitAll(Event{RunID: "x", Type: EventProgress})
	if got := sink.sequences(); len(got) != 2 || got[1] != 2 {
		t.Fatalf("second EmitAll want seq 2, got %v", got)
	}
}
