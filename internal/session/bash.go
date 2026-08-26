package session

import (
	"fmt"
	"strings"
)

// BashContextText is the user-message text injected into the next LLM prompt
// for a bashExecution entry. packages/coding-agent/src/core/messages.ts bashExecutionToText
func BashContextText(command, output string, cancelled bool, exitCode *int, truncated bool, fullOutputPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Ran `%s`\n", command)
	if output != "" {
		fmt.Fprintf(&b, "```\n%s\n```", output)
	} else {
		b.WriteString("(no output)")
	}
	if cancelled {
		b.WriteString("\n\n(command cancelled)")
	} else if exitCode != nil && *exitCode != 0 {
		fmt.Fprintf(&b, "\n\nCommand exited with code %d", *exitCode)
	}
	if truncated && fullOutputPath != "" {
		fmt.Fprintf(&b, "\n\n[Output truncated. Full output: %s]", fullOutputPath)
	}
	return b.String()
}
