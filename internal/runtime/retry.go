// Package runtime contains the shared agent engine used by TUI, print, JSON, and RPC.
package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
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
	if e.willOverflowRetry(ev.Messages) {
		return true
	}
	if !e.Opts.Config.RetryEnabled() || e.retryAttempt >= e.Opts.Config.RetryMaxRetries() {
		return false
	}
	return ai.IsRetryableError(lastAssistantMsg(ev.Messages), e.contextWindow())
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

func (e *Engine) withSummarizationRetry(ctx context.Context, source map[string]any, run func() error) error {
	var attempt int
	maxAttempts := e.Opts.Config.RetryMaxRetries()
	var lastRetry bool
	for {
		err := run()
		if err == nil {
			if lastRetry {
				e.emitSession(map[string]any{"type": "summarization_retry_finished"})
			}
			return nil
		}
		if !e.shouldRetrySummarization(err) || attempt >= maxAttempts {
			if lastRetry {
				e.emitSession(map[string]any{"type": "summarization_retry_finished"})
			}
			return err
		}
		attempt++
		lastRetry = true
		delayMs := e.Opts.Config.RetryBaseDelayMs() * (1 << (attempt - 1))
		e.emitSession(map[string]any{
			"type":         "summarization_retry_scheduled",
			"attempt":      attempt,
			"maxAttempts":  maxAttempts,
			"delayMs":      delayMs,
			"errorMessage": summarizationErrorMessage(err),
		})
		if sleepErr := e.sleepRetry(ctx, time.Duration(delayMs)*time.Millisecond); sleepErr != nil {
			e.emitSession(map[string]any{"type": "summarization_retry_finished"})
			return sleepErr
		}
		ev := map[string]any{"type": "summarization_retry_attempt_start"}
		for k, v := range source {
			ev[k] = v
		}
		e.emitSession(ev)
	}
}

func (e *Engine) shouldRetrySummarization(err error) bool {
	if err == nil || !e.Opts.Config.RetryEnabled() {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, compaction.ErrSummarizeAborted) || errors.Is(err, compaction.ErrSummaryAborted) {
		return false
	}
	msg := &ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: summarizationErrorMessage(err)}
	return ai.IsRetryableAssistantError(msg)
}

func summarizationErrorMessage(err error) string {
	var se *compaction.SummarizeError
	if errors.As(err, &se) && se.Cause != "" {
		return se.Cause
	}
	if err == nil {
		return "Unknown error"
	}
	if err.Error() == "" {
		return "Unknown error"
	}
	return err.Error()
}
