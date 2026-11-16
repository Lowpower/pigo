package codemode

import (
	"context"
	"sync/atomic"
)

type stopKey struct{}

// stopHandle is installed on the host context for one script. RequestStop
// records the first message and asks the parent to interrupt the worker.
type stopHandle struct {
	signal func()
	msg    atomic.Value // string
}

func withStop(ctx context.Context, signal func()) (context.Context, *stopHandle) {
	h := &stopHandle{signal: signal}
	return context.WithValue(ctx, stopKey{}, h), h
}

func (h *stopHandle) message() string {
	if h == nil {
		return ""
	}
	s, _ := h.msg.Load().(string)
	return s
}

// RequestStop interrupts the worker running ctx and keeps the first message
// as the script error. A context that is not running a script ignores it.
// Further host calls must not keep appending output after this returns.
func RequestStop(ctx context.Context, message string) {
	if ctx == nil {
		return
	}
	h, _ := ctx.Value(stopKey{}).(*stopHandle)
	if h == nil {
		return
	}
	if message != "" {
		h.msg.CompareAndSwap(nil, message)
	}
	if h.signal != nil {
		h.signal()
	}
}
