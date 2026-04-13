package hooks

import (
	"context"
	"testing"
)

func TestHookFireFromContext(t *testing.T) {
	var called bool
	ctx := WithHookFire(context.Background(), func(ctx context.Context, p Payload) ([]Result, error) {
		called = true
		if p.Event != EventSubagentStart {
			t.Errorf("event = %q", p.Event)
		}
		return nil, nil
	})
	fn := HookFireFromContext(ctx)
	if fn == nil {
		t.Fatal("expected hook fire")
	}
	_, _ = fn(ctx, Payload{Event: EventSubagentStart})
	if !called {
		t.Fatal("hook not invoked")
	}
	if HookFireFromContext(context.Background()) != nil {
		t.Fatal("expected nil without WithHookFire")
	}
}
