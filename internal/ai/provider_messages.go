package ai

import "encoding/json"

// AnthropicWireMessages maps pigo messages onto the Anthropic Messages API
// shape (pi packages/ai/src/api/anthropic-messages.ts). Assistant tool calls
// become tool_use blocks; toolResult becomes a user message with tool_result.
func AnthropicWireMessages(msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if m.Assistant != nil {
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": anthropicContent(m.Assistant),
			})
			continue
		}
		if m.Role == RoleToolResult || m.ToolCallID != "" {
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Content,
			}
			if m.IsError {
				block["is_error"] = true
			}
			out = append(out, map[string]any{
				"role":    "user",
				"content": []map[string]any{block},
			})
			continue
		}
		role := m.Role
		if role == "" {
			role = RoleUser
		}
		out = append(out, map[string]any{"role": role, "content": m.Content})
	}
	return out
}

func anthropicContent(msg *AssistantMessage) []map[string]any {
	blocks := make([]map[string]any, 0, len(msg.Content))
	for _, c := range msg.Content {
		switch c.Type {
		case KindText:
			blocks = append(blocks, map[string]any{"type": "text", "text": c.Text})
		case KindThinking:
			b := map[string]any{"type": "thinking", "thinking": c.Thinking}
			if c.ThinkingSignature != "" {
				b["signature"] = c.ThinkingSignature
			}
			if c.Redacted {
				b["type"] = "redacted_thinking"
				b["data"] = c.Thinking
			}
			blocks = append(blocks, b)
		case KindToolCall:
			input := any(c.Arguments)
			if input == nil {
				input = map[string]any{}
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    c.ToolID,
				"name":  c.ToolName,
				"input": input,
			})
		}
	}
	if len(blocks) == 0 && msg.Text() != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": msg.Text()})
	}
	return blocks
}

// OpenAIWireMessages maps pigo messages onto OpenAI Chat Completions
// (pi packages/ai/src/api/openai-completions.ts). toolResult becomes role=tool
// with tool_call_id; assistant tool calls become tool_calls.
func OpenAIWireMessages(msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if m.Assistant != nil {
			out = append(out, openaiAssistant(m.Assistant))
			continue
		}
		if m.Role == RoleToolResult || m.ToolCallID != "" {
			out = append(out, map[string]any{
				"role":         "tool",
				"tool_call_id": m.ToolCallID,
				"content":      m.Content,
			})
			continue
		}
		role := m.Role
		if role == RoleTool {
			role = "tool"
		}
		out = append(out, map[string]any{"role": role, "content": m.Content})
	}
	return out
}

func openaiAssistant(msg *AssistantMessage) map[string]any {
	var text string
	var toolCalls []map[string]any
	for _, c := range msg.Content {
		switch c.Type {
		case KindText:
			text += c.Text
		case KindToolCall:
			args, _ := json.Marshal(c.Arguments)
			if string(args) == "null" {
				args = []byte("{}")
			}
			toolCalls = append(toolCalls, map[string]any{
				"id":   c.ToolID,
				"type": "function",
				"function": map[string]any{
					"name":      c.ToolName,
					"arguments": string(args),
				},
			})
		}
	}
	out := map[string]any{"role": "assistant", "content": text}
	if len(toolCalls) > 0 {
		out["tool_calls"] = toolCalls
	}
	return out
}
