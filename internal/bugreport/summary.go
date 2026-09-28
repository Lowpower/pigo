package bugreport

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
)

// ErrCancelled is returned when the user cancels summary generation.
var ErrCancelled = errors.New("bug report summary was cancelled")

const summarySystemPrompt = `You are helping a user file a bug report about pigo, the coding agent they are talking to. You will be shown the conversation transcript. Write a report for the pigo developers describing what the user was doing and what went wrong.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the report.`

const summaryInstructions = `Write the bug report in Markdown with these sections:

## What the user was doing
One short paragraph.

## What went wrong
Concrete description of the failure: wrong output, errors, hangs, tool failures, unexpected behavior. Quote error messages and tool output verbatim where they exist.

## Steps to reproduce
Numbered list, as specific as the transcript allows.

## Relevant details
Tool calls involved, files touched, model behavior, anything else that helps a developer reproduce or locate the problem.

Do not include file contents, secrets, or credentials from the transcript; refer to files by path only. Keep the report factual and concise.`

// SummarySystemPrompt is the system prompt for /bug --summary.
func SummarySystemPrompt() string { return summarySystemPrompt }

// SelectMessages keeps the newest messages that fit in tokenBudget.
// The newest message is kept even when it alone exceeds the budget.
func SelectMessages(msgs []ai.Message, tokenBudget int) []ai.Message {
	if tokenBudget <= 0 {
		tokenBudget = 8000
	}
	var selected []ai.Message
	tokens := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		next := compaction.EstimateTokens(msgs[i])
		if len(selected) > 0 && tokens+next > tokenBudget {
			break
		}
		selected = append(selected, msgs[i])
		tokens += next
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
}

// SummaryUserPrompt is the user turn sent to the current model.
func SummaryUserPrompt(msgs []ai.Message, hint string, total int) string {
	var b strings.Builder
	if len(msgs) < total {
		fmt.Fprintf(&b, "Note: only the last %d of %d messages are shown.\n\n", len(msgs), total)
	}
	b.WriteString("<conversation>\n")
	for _, m := range msgs {
		role := m.Role
		if role == "" {
			role = "message"
		}
		text := m.Text()
		if text == "" && m.Assistant != nil {
			text = m.Assistant.Text()
			if text == "" && m.Assistant.ErrorMessage != "" {
				text = m.Assistant.ErrorMessage
			}
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(text)
		b.WriteByte('\n')
	}
	b.WriteString("</conversation>")
	if strings.TrimSpace(hint) != "" {
		b.WriteString("\n\n<user-report>\n")
		b.WriteString(strings.TrimSpace(hint))
		b.WriteString("\n</user-report>")
	}
	b.WriteString("\n\n")
	b.WriteString(summaryInstructions)
	return b.String()
}
