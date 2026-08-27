package tools

import "context"

type outputUpdateKey struct{}

// WithOutputUpdate registers a callback for accumulated streaming tool output.
func WithOutputUpdate(ctx context.Context, fn func(accumulated string)) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, outputUpdateKey{}, fn)
}

// OutputUpdate is the streaming callback stored on ctx, or nil.
func OutputUpdate(ctx context.Context) func(string) {
	fn, _ := ctx.Value(outputUpdateKey{}).(func(string))
	return fn
}
