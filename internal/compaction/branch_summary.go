package compaction

import (
	"context"
	"errors"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
)

// ErrSummaryAborted is returned when the summarizer stopReason is aborted.
var ErrSummaryAborted = errors.New("branch summary aborted")

// ErrSummarizeAborted is returned when compaction summarization is aborted.
var ErrSummarizeAborted = errors.New("compaction: summarization aborted")

// SummarizeError is a failed (non-abort) summarization call.
type SummarizeError struct {
	Cause string
}

func (e *SummarizeError) Error() string {
	if e == nil || e.Cause == "" {
		return "compaction: summarization failed"
	}
	return "compaction: summarization failed: " + e.Cause
}

// BranchSummaryPreamble is prepended to the stored summary so later turns
// know the text describes an abandoned branch.
const BranchSummaryPreamble = `The user explored a different conversation branch before returning here.
Summary of that exploration:

`

// BranchSummaryPrompt is the default instruction given to the summarizer.
const BranchSummaryPrompt = `The messages above are earlier context from an ongoing conversation. Later messages are stored separately and do not need to be reconstructed.

Create a structured summary of this conversation branch for context when returning later.

Use this EXACT format:

## Goal
[What was the user trying to accomplish in this branch?]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Work that was started but not finished]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [What should happen next to continue this work]

Keep each section concise. Preserve exact file paths, function names, and error messages.

Only summarize information explicitly present above. Do not infer or recreate later messages.`

// BranchSummaryOpts controls generateBranchSummary.
type BranchSummaryOpts struct {
	CustomInstructions  string
	ReplaceInstructions bool
	ReserveTokens       int
	ContextWindow       int
	// Provider selects the catalog entry whose samplingParams go out with the summary request.
	Provider string
	// PriorRead and PriorModified come from earlier branch summaries that were
	// not produced by an extension. They are merged even when those entries do
	// not fit in the token budget.
	PriorRead     []string
	PriorModified []string
}

// NoContentSummary is stored when a branch has no model-visible messages.
// That path does not call the model, so its usage is zero and must not be
// written onto the entry.
const NoContentSummary = "No content to summarize"

// GenerateBranchSummary asks the model to summarize abandoned-branch entries.
// The returned lists include prior branch-summary files plus tool calls from
// the messages that fit in the token budget. The usage is the summary
// request's usage; it is zero when no request was made.
func GenerateBranchSummary(ctx context.Context, sf ai.StreamFn, model string, msgs []ai.Message, opts BranchSummaryOpts) (string, FileLists, ai.Usage, error) {
	reserve := opts.ReserveTokens
	if reserve <= 0 {
		reserve = 16384
	}
	window := opts.ContextWindow
	if window <= 0 {
		window = 128000
	}
	budget := window - reserve
	if budget < 1 {
		budget = 1
	}

	var kept []ai.Message
	acc := 0
	oldest := len(msgs)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == ai.RoleToolResult || msgs[i].Role == ai.RoleTool {
			continue
		}
		n := EstimateTokens(msgs[i])
		if acc+n > budget && len(kept) > 0 {
			break
		}
		kept = append(kept, msgs[i])
		oldest = i
		acc += n
	}
	if len(kept) == 0 {
		return NoContentSummary, FileLists{}, ai.Usage{}, nil
	}

	instructions := BranchSummaryPrompt
	if opts.ReplaceInstructions && opts.CustomInstructions != "" {
		instructions = opts.CustomInstructions
	} else if opts.CustomInstructions != "" {
		instructions = BranchSummaryPrompt + "\n\nAdditional focus: " + opts.CustomInstructions
	}
	prompt := "# Conversation\n" + serializeConversation(msgs[oldest:]) + "\n\n# Instructions\n" + instructions

	stream, err := sf(ctx, ai.Context{
		System:   SummarizationSystemPrompt,
		Messages: []ai.Message{{Role: ai.RoleUser, Content: prompt}},
	}, modelCallOptions(opts.Provider, model, reserve))
	if err != nil {
		return "", FileLists{}, ai.Usage{}, err
	}
	_, final := stream.Collect()
	if final == nil {
		return "", FileLists{}, ai.Usage{}, errors.New("branch summary produced no message")
	}
	if final.StopReason == ai.StopAborted {
		if err := ctx.Err(); err != nil {
			return "", FileLists{}, ai.Usage{}, err
		}
		return "", FileLists{}, ai.Usage{}, ErrSummaryAborted
	}
	if final.StopReason == ai.StopError {
		return "", FileLists{}, ai.Usage{}, &SummarizeError{Cause: final.ErrorMessage}
	}
	if final.StopReason == ai.StopLength {
		return "", FileLists{}, ai.Usage{}, incompleteSummaryError("Branch summary")
	}
	text := strings.TrimSpace(final.Text())
	if text == "" {
		text = "No summary generated"
	}
	files := CollectFileLists(kept, opts.PriorRead, opts.PriorModified, true)
	text = AppendFileSections(BranchSummaryPreamble+text, files)
	return text, files, final.Usage, nil
}
