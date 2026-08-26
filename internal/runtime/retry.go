// Package runtime contains the shared agent engine used by TUI, print, JSON, and RPC.
package runtime

import (
	"context"
	"time"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
)

// Ported from pi packages/coding-agent/src/core/agent-session.ts
// (_prepareRetry / abortRetry / setAutoRetryEnabled / _willRetryAfterAgentEnd).

// SetAutoRetryEnabled toggles settings.retry.enabled (in-memory; disk overlay is #14).
func (e *Engine) SetAutoRetryEnabled(enabled bool) {
	on := enabled
	e.Opts.Config.Retry.Enabled = &on
}

// AbortRetry cancels an in-progress retry backoff sleep.
func (e *Engine) AbortRetry() {
	e.retryMu.Lock()
	defer e.retryMu.Unlock()
	if e.retryCancel != nil {
		e.retryCancel()
	}
}

func (e *Engine) willRetryAfterAgentEnd(ev agent.Event) bool {
	if !e.Opts.Config.RetryEnabled() || e.retryAttempt >= e.Opts.Config.RetryMaxRetries() {
		return false
	}
	return ai.IsRetryableError(lastAssistantMsg(ev.Messages), e.contextWindow())
}

func (e *Engine) contextWindow() int {
	if e.Opts.ContextWindow > 0 {
		return e.Opts.ContextWindow
	}
	return e.Opts.Config.ContextWindow
}

func (e *Engine) prepareRetry(ctx context.Context, last []agent.Msg) bool {
	msg := lastAssistantMsg(last)
	if !ai.IsRetryableError(msg, e.contextWindow()) {
		e.emitRetryFailure(msg)
		return false
	}
	if !e.Opts.Config.RetryEnabled() {
		return false
	}

	e.retryAttempt++
	maxAttempts := e.Opts.Config.RetryMaxRetries()
	if e.retryAttempt > maxAttempts {
		e.retryAttempt--
		e.emitRetryFailure(msg)
		return false
	}

	delayMs := e.Opts.Config.RetryBaseDelayMs()
	if e.retryAttempt > 1 {
		delayMs *= 1 << (e.retryAttempt - 1)
	}
	errMsg := msg.ErrorMessage
	if errMsg == "" {
		errMsg = "Unknown error"
	}
	e.emitSession(map[string]any{
		"type":         "auto_retry_start",
		"attempt":      e.retryAttempt,
		"maxAttempts":  maxAttempts,
		"delayMs":      delayMs,
		"errorMessage": errMsg,
	})
	if err := e.sleepRetry(ctx, time.Duration(delayMs)*time.Millisecond); err != nil {
		attempt := e.retryAttempt
		e.retryAttempt = 0
		e.emitSession(map[string]any{
			"type":       "auto_retry_end",
			"success":    false,
			"attempt":    attempt,
			"finalError": "Retry cancelled",
		})
		return false
	}
	return true
}

func (e *Engine) emitRetryFailure(msg *ai.AssistantMessage) {
	if msg == nil || msg.StopReason != ai.StopError || e.retryAttempt <= 0 {
		return
	}
	e.emitSession(map[string]any{
		"type":       "auto_retry_end",
		"success":    false,
		"attempt":    e.retryAttempt,
		"finalError": msg.ErrorMessage,
	})
	e.retryAttempt = 0
}

func (e *Engine) sleepRetry(ctx context.Context, d time.Duration) error {
	ctx, cancel := context.WithCancel(ctx)
	e.retryMu.Lock()
	e.retryCancel = cancel
	e.retryMu.Unlock()
	defer func() {
		e.retryMu.Lock()
		e.retryCancel = nil
		e.retryMu.Unlock()
		cancel()
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func lastAssistantMsg(msgs []agent.Msg) *ai.AssistantMessage {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == agent.RoleAssistant {
			return msgs[i].Assistant
		}
	}
	return nil
}

func stripLastAssistant(msgs []agent.Msg) []ai.Message {
	hist := agent.MessagesFromTranscript(msgs)
	if n := len(hist); n > 0 && hist[n-1].Role == ai.RoleAssistant {
		return hist[:n-1]
	}
	return hist
}
