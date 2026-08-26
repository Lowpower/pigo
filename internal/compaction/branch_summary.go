package compaction

import (
	"context"
	"errors"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
)

// ErrSummaryAborted is returned when the summarizer stopReason is aborted.
var ErrSummaryAborted = errors.New("branch summary aborted")

// BranchSummaryPreamble is prepended to the stored summary so later turns
// know the text describes an abandoned branch.
const BranchSummaryPreamble = `The user explored a different conversation branch before returning here.
Summary of that exploration:

`

// BranchSummaryPrompt is the default instruction given to the summarizer.
const BranchSummaryPrompt = `Create a structured summary of this conversation branch for context when returning later.

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

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// BranchSummaryOpts controls generateBranchSummary.
type BranchSummaryOpts struct {
	CustomInstructions  string
	ReplaceInstructions bool
	ReserveTokens       int
	ContextWindow       int
}

// GenerateBranchSummary asks the model to summarize abandoned-branch entries.
func GenerateBranchSummary(ctx context.Context, sf ai.StreamFn, model string, msgs []ai.Message, opts BranchSummaryOpts) (string, error) {
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
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == ai.RoleToolResult {
			continue
		}
		n := EstimateTokens(msgs[i])
		if acc+n > budget && len(kept) > 0 {
			break
		}
		kept = append(kept, msgs[i])
		acc += n
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	if len(kept) == 0 {
		return "No content to summarize", nil
	}

	var conv strings.Builder
	for _, m := range kept {
		conv.WriteString(string(m.Role))
		conv.WriteString(": ")
		conv.WriteString(m.Text())
		conv.WriteByte('\n')
	}

	instructions := BranchSummaryPrompt
	if opts.ReplaceInstructions && opts.CustomInstructions != "" {
		instructions = opts.CustomInstructions
	} else if opts.CustomInstructions != "" {
		instructions = BranchSummaryPrompt + "\n\nAdditional focus: " + opts.CustomInstructions
	}
	prompt := "<conversation>\n" + conv.String() + "</conversation>\n\n" + instructions

	stream, err := sf(ctx, ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: prompt}}}, ai.Options{Model: model})
	if err != nil {
		return "", err
	}
	_, final := stream.Collect()
	if final == nil {
		return "", errors.New("branch summary produced no message")
	}
	if final.StopReason == ai.StopAborted {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", ErrSummaryAborted
	}
	if final.StopReason == ai.StopError {
		return "", errors.New("branch summary failed: " + final.ErrorMessage)
	}
	text := strings.TrimSpace(final.Text())
	if text == "" {
		text = "No summary generated"
	}
	return BranchSummaryPreamble + text, nil
}
