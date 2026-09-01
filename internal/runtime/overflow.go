package runtime

import (
	"context"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
)

func (e *Engine) willOverflowRetry(last []agent.Msg) bool {
	msg := lastAssistantMsg(last)
	if !e.overflowShouldHandle(msg) {
		return false
	}
	if msg.StopReason == ai.StopStop {
		return false
	}
	return !e.overflowAttempted
}

func (e *Engine) overflowShouldHandle(msg *ai.AssistantMessage) bool {
	if msg == nil || !e.Opts.Config.CompactionEnabled() {
		return false
	}
	if ai.IsContextOverflow(msg, e.contextWindow()) {
		return true
	}
	return ai.IsRecoverableLength(msg, e.maxTokens())
}

func (e *Engine) prepareOverflow(ctx context.Context, last []agent.Msg) bool {
	msg := lastAssistantMsg(last)
	if !e.overflowShouldHandle(msg) {
		return false
	}
	willRetry := msg.StopReason != ai.StopStop
	if !willRetry {
		_, _, _ = e.runCompaction(ctx, "overflow", agent.MessagesFromTranscript(last), e.compactionSettings(), false)
		return false
	}
	if e.overflowAttempted {
		errMsg := "Context overflow recovery failed after one compact-and-retry attempt. Try reducing context or switching to a larger-context model."
		if !ai.IsContextOverflow(msg, e.contextWindow()) {
			errMsg = "Truncated response recovery failed after one compact-and-retry attempt."
		}
		e.emitSession(map[string]any{
			"type": "compaction_end", "reason": "overflow",
			"result": nil, "aborted": false, "willRetry": false, "errorMessage": errMsg,
		})
		return false
	}
	e.overflowAttempted = true
	hist := stripLastAssistant(last)
	_, _, err := e.runCompaction(ctx, "overflow", hist, e.compactionSettings(), true)
	return err == nil
}

// Continue reruns the agent loop on an existing history (retry / overflow).
func (e *Engine) Continue(ctx context.Context, hist []ai.Message) *agent.Stream {
	return e.runLoop(ctx, hist, nil)
}

// AfterAgentEnd applies overflow compact-and-retry then auto-retry.
func (e *Engine) AfterAgentEnd(ctx context.Context, last []agent.Msg) (hist []ai.Message, again bool) {
	if e.prepareOverflow(ctx, last) {
		return stripLastAssistant(last), true
	}
	if e.prepareRetry(ctx, last) {
		return stripLastAssistant(last), true
	}
	return nil, false
}

// PersistTurn writes new session entries, using prefix after a retry/overflow continue.
func (e *Engine) PersistTurn(last []agent.Msg, prefix int) {
	if prefix > 0 {
		if prefix > len(last) {
			prefix = len(last)
		}
		e.persisted = 0
		e.PersistTranscript(last[prefix:])
		return
	}
	e.PersistTranscript(last)
}
