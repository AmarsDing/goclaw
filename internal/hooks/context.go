package hooks

import "context"

type hookFireKey struct{}

// HookFire is invoked by tools (e.g. delegate) to run hooks with the same contract as Registry.Fire.
type HookFire func(ctx context.Context, payload Payload) ([]Result, error)

// WithHookFire attaches a hook dispatcher to the context so tools can emit pipeline hooks
// without importing the agent loop (e.g. subagent.start / subagent.stop for delegation).
func WithHookFire(ctx context.Context, fn HookFire) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, hookFireKey{}, fn)
}

// HookFireFromContext returns the hook dispatcher, or nil.
func HookFireFromContext(ctx context.Context) HookFire {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(hookFireKey{}).(HookFire)
	return fn
}
