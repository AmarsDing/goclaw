package hooks

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestRegistry_EnabledFalseSkipsHook(t *testing.T) {
	reg := NewRegistry()
	var calls atomic.Int32
	reg.RegisterInternal("mark", func(ctx context.Context, payload Payload) (*Result, error) {
		calls.Add(1)
		return &Result{Success: true}, nil
	})

	if err := reg.Register(Hook{
		ID:      "off",
		Event:   EventSessionStart,
		Mode:    ModeSync,
		Enabled: false,
		Handler: HandlerSpec{Type: HandlerInternal, Target: "mark"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(Hook{
		ID:      "on",
		Event:   EventSessionStart,
		Mode:    ModeSync,
		Enabled: true,
		Handler: HandlerSpec{Type: HandlerInternal, Target: "mark"},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := reg.Fire(context.Background(), Payload{Event: EventSessionStart})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("want 1 internal handler call, got %d", calls.Load())
	}
}

func TestRegistry_DefaultEnabledZeroMeansDisabled(t *testing.T) {
	reg := NewRegistry()
	var calls atomic.Int32
	reg.RegisterInternal("x", func(ctx context.Context, payload Payload) (*Result, error) {
		calls.Add(1)
		return &Result{Success: true}, nil
	})
	// Enabled omitted → false → hook does not run (callers must set Enabled: true explicitly).
	if err := reg.Register(Hook{
		ID:      "implicit",
		Event:   EventSessionEnd,
		Mode:    ModeSync,
		Handler: HandlerSpec{Type: HandlerInternal, Target: "x"},
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = reg.Fire(context.Background(), Payload{Event: EventSessionEnd})
	if calls.Load() != 0 {
		t.Fatalf("zero-value Enabled should skip hook; got %d calls", calls.Load())
	}
}
