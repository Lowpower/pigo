package agent

import "context"

type emitterKey struct{}

// WithToolEmitter stores the loop's event emitter on ctx so a nested tool
// call can emit tool_execution events with a parent id.
func WithToolEmitter(ctx context.Context, fn func(Event) bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, emitterKey{}, fn)
}

// ToolEmitter returns the emitter stored by WithToolEmitter.
func ToolEmitter(ctx context.Context) func(Event) bool {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(emitterKey{}).(func(Event) bool)
	return fn
}
