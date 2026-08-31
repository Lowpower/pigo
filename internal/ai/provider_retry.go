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

// WrapProviderRetry retries StreamFn setup failures (not overflow) with backoff.
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
		defer cancel()

		var last error
		for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
			stream, err := inner(callCtx, reqCtx, opts)
			if err == nil {
				return stream, nil
			}
			last = err
			if attempt == cfg.MaxRetries || !isProviderSetupRetryable(err) {
				return nil, err
			}
			delay := providerBackoff(attempt, cfg.MaxDelay)
			if delay < 0 {
				delay = 0
			}
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-callCtx.Done():
				timer.Stop()
				return nil, callCtx.Err()
			}
			timer.Stop()
		}
		if last == nil {
			last = errors.New("provider retry exhausted")
		}
		return nil, last
	}
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

func providerBackoff(attempt int, maxDelay time.Duration) time.Duration {
	d := time.Duration(500*(1<<attempt)) * time.Millisecond
	if maxDelay > 0 && d > maxDelay {
		return maxDelay
	}
	return d
}
