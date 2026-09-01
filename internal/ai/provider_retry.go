package ai

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ProviderRetry is settings.retry.provider applied to a single StreamFn call.
type ProviderRetry struct {
	Timeout    time.Duration
	MaxRetries int
	MaxDelay   time.Duration
}

// WrapProviderRetry retries StreamFn setup failures and retryable in-stream
// errors that produced no output (not overflow) with backoff.
func WrapProviderRetry(inner StreamFn, cfg ProviderRetry) StreamFn {
	if inner == nil {
		return nil
	}
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		callCtx := ctx
		cancel := func() {}
		if cfg.Timeout > 0 {
			callCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		}

		stream, err := inner(callCtx, reqCtx, opts)
		for attempt := 0; err != nil; attempt++ {
			if attempt == cfg.MaxRetries || !isProviderSetupRetryable(err) {
				cancel()
				return nil, err
			}
			if sleepErr := sleepProviderRetry(callCtx, attempt, cfg.MaxDelay); sleepErr != nil {
				cancel()
				return nil, sleepErr
			}
			stream, err = inner(callCtx, reqCtx, opts)
		}

		out := NewEventStream(16)
		go func() {
			defer cancel()
			defer out.end()
			current := stream
			attemptsLeft := cfg.MaxRetries
			for {
				var buf []Event
				var terminal *AssistantMessage
				closed := true
				for ev := range current.Events() {
					if eventHasOutput(ev) {
						for _, b := range buf {
							if !out.push(callCtx, b) {
								return
							}
						}
						if !out.push(callCtx, ev) {
							return
						}
						for ev := range current.Events() {
							if !out.push(callCtx, ev) {
								return
							}
						}
						return
					}
					buf = append(buf, ev)
					if ev.Type == EventDone || ev.Type == EventError {
						terminal = ev.Message
						closed = false
						break
					}
				}
				if attemptsLeft > 0 && streamErrorRetryable(terminal) {
					attemptsLeft--
					attempt := cfg.MaxRetries - attemptsLeft - 1
					if attempt < 0 {
						attempt = 0
					}
					if sleepErr := sleepProviderRetry(callCtx, attempt, cfg.MaxDelay); sleepErr != nil {
						return
					}
					next, nerr := inner(callCtx, reqCtx, opts)
					if nerr != nil {
						if isProviderSetupRetryable(nerr) && attemptsLeft > 0 {
							current = EmitMessage(callCtx, &AssistantMessage{Role: RoleAssistant, StopReason: StopError, ErrorMessage: nerr.Error()})
							continue
						}
						msg := &AssistantMessage{Role: RoleAssistant, StopReason: StopError, ErrorMessage: nerr.Error()}
						out.push(callCtx, Event{Type: EventError, Reason: StopError, Message: msg})
						return
					}
					current = next
					continue
				}
				for _, b := range buf {
					if !out.push(callCtx, b) {
						return
					}
				}
				if !closed {
					for ev := range current.Events() {
						if !out.push(callCtx, ev) {
							return
						}
					}
				}
				return
			}
		}()
		return out, nil
	}
}

func eventHasOutput(ev Event) bool {
	switch ev.Type {
	case EventTextStart, EventTextDelta, EventThinkingStart, EventThinkingDelta,
		EventToolCallStart, EventToolCallDelta, EventToolCallEnd:
		return true
	default:
		return false
	}
}

func streamErrorRetryable(msg *AssistantMessage) bool {
	if msg == nil {
		return false
	}
	if IsContextOverflow(msg, 0) {
		return false
	}
	return IsRetryableAssistantError(msg)
}

func isProviderSetupRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	msg := &AssistantMessage{StopReason: StopError, ErrorMessage: err.Error()}
	if IsContextOverflow(msg, 0) {
		return false
	}
	if IsRetryableAssistantError(msg) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "timeout") || strings.Contains(s, "temporarily") || strings.Contains(s, "eof")
}

func sleepProviderRetry(ctx context.Context, attempt int, maxDelay time.Duration) error {
	delay := providerBackoff(attempt, maxDelay)
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	}
}

func providerBackoff(attempt int, maxDelay time.Duration) time.Duration {
	d := time.Duration(500*(1<<attempt)) * time.Millisecond
	if maxDelay > 0 && d > maxDelay {
		return maxDelay
	}
	return d
}
