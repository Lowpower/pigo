package ai

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
