package ai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProviderRetry is settings.retry.provider applied to a single StreamFn call.
type ProviderRetry struct {
	Timeout    time.Duration
	MaxRetries int
	MaxDelay   time.Duration
}

// providerHTTPSnap is the latest doHTTP response seen by one StreamFn call.
// Headers stay off AssistantMessage so session JSONL does not grow new fields.
type providerHTTPSnap struct {
	mu     sync.Mutex
	seen   bool
	status int
	header http.Header
}

func (s *providerHTTPSnap) record(status int, h http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = true
	s.status = status
	if h == nil {
		s.header = nil
		return
	}
	s.header = h.Clone()
}

func (s *providerHTTPSnap) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = false
	s.status = 0
	s.header = nil
}

func (s *providerHTTPSnap) get() (status int, header http.Header, seen bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.header, s.seen
}

func observeProviderHTTP(opts Options, snap *providerHTTPSnap) Options {
	prev := opts.OnHTTPResponse
	opts.OnHTTPResponse = func(status int, h http.Header) {
		if prev != nil {
			prev(status, h)
		}
		snap.record(status, h)
	}
	return opts
}

// WrapProviderRetry retries StreamFn setup failures and retryable in-stream
// errors that produced no output (not overflow) with backoff.
func WrapProviderRetry(inner StreamFn, cfg ProviderRetry) StreamFn {
	if inner == nil {
		return nil
	}
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		var snap providerHTTPSnap
		opts = observeProviderHTTP(opts, &snap)
		callCtx := ctx
		cancel := func() {}
		if cfg.Timeout > 0 {
			callCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		}

		snap.reset()
		stream, err := inner(callCtx, reqCtx, opts)
		for attempt := 0; err != nil; attempt++ {
			if attempt == cfg.MaxRetries || !isProviderSetupRetryable(err, &snap) {
				cancel()
				return nil, err
			}
			delay, delayErr := delayFromSnap(&snap, attempt, cfg.MaxDelay, err.Error())
			if delayErr != nil {
				cancel()
				return nil, delayErr
			}
			if sleepErr := sleepProviderRetry(callCtx, delay); sleepErr != nil {
				cancel()
				return nil, sleepErr
			}
			snap.reset()
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
				if attemptsLeft > 0 && streamErrorRetryable(terminal, &snap) {
					attemptsLeft--
					attempt := cfg.MaxRetries - attemptsLeft - 1
					if attempt < 0 {
						attempt = 0
					}
					delay, delayErr := delayFromSnap(&snap, attempt, cfg.MaxDelay, assistantErrorText(terminal))
					if delayErr != nil {
						out.push(callCtx, Event{Type: EventError, Reason: StopError, Message: serverDelayMessage(terminal, delayErr)})
						return
					}
					if sleepErr := sleepProviderRetry(callCtx, delay); sleepErr != nil {
						return
					}
					snap.reset()
					next, nerr := inner(callCtx, reqCtx, opts)
					if nerr != nil {
						if isProviderSetupRetryable(nerr, &snap) && attemptsLeft > 0 {
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

func streamErrorRetryable(msg *AssistantMessage, snap *providerHTTPSnap) bool {
	if msg == nil {
		return false
	}
	if IsContextOverflow(msg, 0) {
		return false
	}
	status, header, seen := snap.get()
	return applyHTTPRetry(status, header, seen, IsRetryableAssistantError(msg))
}

func isProviderSetupRetryable(err error, snap *providerHTTPSnap) bool {
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
	messageRetryable := IsRetryableAssistantError(msg)
	if !messageRetryable {
		s := strings.ToLower(err.Error())
		messageRetryable = strings.Contains(s, "timeout") || strings.Contains(s, "temporarily") || strings.Contains(s, "eof")
	}
	status, header, seen := snap.get()
	return applyHTTPRetry(status, header, seen, messageRetryable)
}

// applyHTTPRetry uses x-should-retry, then 408/409/429/5xx, only for a non-2xx
// response. A 2xx that later fails in-stream keeps the message classification.
func applyHTTPRetry(status int, header http.Header, seen, messageRetryable bool) bool {
	if seen && (status < 200 || status >= 300) {
		switch header.Get("X-Should-Retry") {
		case "true":
			return true
		case "false":
			return false
		}
		if status == http.StatusRequestTimeout || status == http.StatusConflict || status == http.StatusTooManyRequests || status >= 500 {
			return true
		}
	}
	return messageRetryable
}

func assistantErrorText(msg *AssistantMessage) string {
	if msg == nil {
		return ""
	}
	return msg.ErrorMessage
}

func serverDelayMessage(terminal *AssistantMessage, err error) *AssistantMessage {
	out := &AssistantMessage{Role: RoleAssistant, StopReason: StopError, ErrorMessage: err.Error()}
	if terminal != nil {
		out.API = terminal.API
		out.Provider = terminal.Provider
		out.Model = terminal.Model
	}
	return out
}

func delayFromSnap(snap *providerHTTPSnap, attempt int, maxDelay time.Duration, providerErr string) (time.Duration, error) {
	status, header, seen := snap.get()
	if !seen || (status >= 200 && status < 300) {
		header = nil
	}
	return providerRetryDelay(header, attempt, maxDelay, time.Now(), providerErr)
}

// providerRetryDelay prefers retry-after-ms, then retry-after seconds or an
// HTTP date. Unusable header values fall back to capped exponential backoff
// with up to 25% jitter. maxDelay rejects an oversized server delay and does
// not cap the fallback.
func providerRetryDelay(header http.Header, attempt int, maxDelay time.Duration, now time.Time, providerErr string) (time.Duration, error) {
	if header != nil {
		if raw := header.Get("Retry-After-Ms"); raw != "" {
			if ms, ok := parseFiniteFloat(raw); ok {
				return validateServerRetryDelay(ms, maxDelay, providerErr)
			}
		}
		if raw := header.Get("Retry-After"); raw != "" {
			if seconds, ok := parseFiniteFloat(raw); ok {
				return validateServerRetryDelay(seconds*1000, maxDelay, providerErr)
			}
			if ms, ok := parseRetryAfterDate(raw, now); ok {
				return validateServerRetryDelay(ms, maxDelay, providerErr)
			}
		}
	}
	return exponentialProviderDelay(attempt), nil
}

func parseFiniteFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func parseRetryAfterDate(s string, now time.Time) (float64, bool) {
	t, err := http.ParseTime(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return float64(t.Sub(now)) / float64(time.Millisecond), true
}

func validateServerRetryDelay(delayMs float64, maxDelay time.Duration, providerErr string) (time.Duration, error) {
	if maxDelay > 0 && delayMs > float64(maxDelay)/float64(time.Millisecond) {
		// The capital S matches pi v1.3.0's provider-retry error text.
		return 0, fmt.Errorf("Server requested %ds retry delay (max: %ds). %s", //nolint:staticcheck // pi error text
			int(math.Ceil(delayMs/1000)),
			int(math.Ceil(float64(maxDelay)/float64(time.Second))),
			providerErr)
	}
	if delayMs < 0 {
		delayMs = 0
	}
	return time.Duration(delayMs * float64(time.Millisecond)), nil
}

func exponentialProviderDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	base := 0.5 * math.Pow(2, float64(attempt))
	if math.IsInf(base, 0) || base > 8 {
		base = 8
	}
	ms := base * 1000 * (1 - rand.Float64()*0.25)
	return time.Duration(ms * float64(time.Millisecond))
}

func sleepProviderRetry(ctx context.Context, delay time.Duration) error {
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
