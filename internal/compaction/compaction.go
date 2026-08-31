package compaction

import (
	"context"
	"errors"

	"github.com/Lowpower/pigo/internal/ai"
)

// Settings controls when and how much to compact.
type Settings struct {
	// ReserveTokens is headroom kept below the context window before compacting.
	ReserveTokens int
	// KeepRecentTokens is roughly how many tokens of recent messages to keep verbatim.
	KeepRecentTokens int
	// CustomInstructions is appended to the summarization prompt (RPC compact).
	CustomInstructions string
}

// DefaultSettings is the built-in compaction window.
func DefaultSettings() Settings {
	return Settings{ReserveTokens: 16384, KeepRecentTokens: 20000}
}

// EstimateTokens approximates a message's token count as ceil(chars/4).
func EstimateTokens(m ai.Message) int {
	return ceilDiv(len(m.Text()), 4)
}

// EstimateContextTokens sums the estimate over all messages.
func EstimateContextTokens(msgs []ai.Message) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m)
	}
	return total
}

// ShouldCompact reports whether the context should be compacted:
// contextTokens > contextWindow - reserveTokens.
func ShouldCompact(contextTokens, contextWindow int, s Settings) bool {
	return contextTokens > contextWindow-s.ReserveTokens
}

// FindCutIndex returns the index i such that msgs[i:] is kept verbatim (roughly
// keepRecentTokens worth) and msgs[:i] is summarized. It scans from the end,
// accumulating tokens, and cuts once the recent tail reaches keepRecentTokens.
// Returns 0 when the whole conversation fits within keepRecentTokens.
func FindCutIndex(msgs []ai.Message, keepRecentTokens int) int {
	acc := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		acc += EstimateTokens(msgs[i])
		if acc >= keepRecentTokens {
			return i
		}
	}
	return 0
}

// SummarizationPrompt is the structured checkpoint prompt. It is appended as the
// final user turn.
const SummarizationPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// SummaryPrefix / SummarySuffix wrap a compaction summary for the LLM.
const SummaryPrefix = `The conversation history before this point was compacted into the following summary:

<summary>
`

// SummarySuffix closes the compaction summary wrapper.
const SummarySuffix = `
</summary>`

// BranchSummaryPrefix wraps a branch summary for the LLM.
const BranchSummaryPrefix = `The following is a summary of a branch that this conversation came back from:

<summary>
`

// BranchSummarySuffix closes the branch summary wrapper.
const BranchSummarySuffix = `</summary>`

// SummaryMarker prefixes the synthetic message that replaces compacted history.
const SummaryMarker = SummaryPrefix

// Summarize asks the model to summarize the given messages using StreamFn,
// returning the assistant's text.
func Summarize(ctx context.Context, sf ai.StreamFn, model string, toSummarize []ai.Message, extra string) (string, error) {
	reqMsgs := make([]ai.Message, 0, len(toSummarize)+1)
	reqMsgs = append(reqMsgs, toSummarize...)
	prompt := SummarizationPrompt
	if extra != "" {
		prompt = prompt + "\n\nAdditional focus: " + extra
	}
	reqMsgs = append(reqMsgs, ai.Message{Role: ai.RoleUser, Content: prompt})

	stream, err := sf(ctx, ai.Context{Messages: reqMsgs}, ai.Options{Model: model})
	if err != nil {
		return "", err
	}
	_, final := stream.Collect()
	if final == nil {
		return "", errors.New("compaction: summarization produced no message")
	}
	if final.StopReason == ai.StopAborted {
		return "", ErrSummarizeAborted
	}
	if final.StopReason == ai.StopError {
		return "", &SummarizeError{Cause: final.ErrorMessage}
	}
	summary := final.Text()
	if summary == "" {
		return "", errors.New("compaction: summarization produced empty text")
	}
	return summary, nil
}

// Compact summarizes older messages and returns the compacted message list (a
// single summary message followed by the recent tail) plus the summary. When
// nothing needs summarizing it returns the input unchanged with an empty summary.
func Compact(ctx context.Context, sf ai.StreamFn, model string, msgs []ai.Message, s Settings) ([]ai.Message, string, error) {
	cut := FindCutIndex(msgs, s.KeepRecentTokens)
	if cut <= 0 {
		return msgs, "", nil
	}
	summary, err := Summarize(ctx, sf, model, msgs[:cut], s.CustomInstructions)
	if err != nil {
		return msgs, "", err
	}
	compacted := make([]ai.Message, 0, len(msgs)-cut+1)
	compacted = append(compacted, ai.Message{Role: ai.RoleUser, Content: SummaryPrefix + summary + SummarySuffix})
	compacted = append(compacted, msgs[cut:]...)
	return compacted, summary, nil
}

func ceilDiv(a, b int) int {
	if a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}
