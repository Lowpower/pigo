package ai

import "strings"

func inferCopilotInitiator(messages []Message) string {
	if len(messages) == 0 {
		return "user"
	}
	last := messages[len(messages)-1]
	if last.Role != RoleUser {
		return "agent"
	}
	return "user"
}

func hasCopilotVisionInput(messages []Message) bool {
	for _, m := range messages {
		if m.Role != RoleUser && m.Role != RoleToolResult && m.ToolCallID == "" {
			continue
		}
		if len(m.Images) > 0 {
			return true
		}
	}
	return false
}

func buildCopilotDynamicHeaders(messages []Message) map[string]string {
	headers := map[string]string{
		"X-Initiator":   inferCopilotInitiator(messages),
		"Openai-Intent": "conversation-edits",
	}
	if hasCopilotVisionInput(messages) {
		headers["Copilot-Vision-Request"] = "true"
	}
	return headers
}

func guessCopilotAPI(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(id, "claude"):
		return "anthropic-messages"
	case strings.HasPrefix(id, "gpt-5"),
		strings.HasPrefix(id, "o1"),
		strings.HasPrefix(id, "o3"),
		strings.HasPrefix(id, "o4"),
		strings.Contains(id, "codex"),
		strings.HasPrefix(id, "grok"):
		return "openai-responses"
	default:
		return ""
	}
}
