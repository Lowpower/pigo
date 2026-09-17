package ai

import "encoding/json"

// AnthropicWireMessages maps pigo messages onto the Anthropic Messages API
// shape. Assistant tool calls become tool_use blocks; toolResult becomes a user
// message with tool_result.
func AnthropicWireMessages(msgs []Message) []map[string]any {
	return anthropicWireMessages(msgs, false, nil)
}

func anthropicWireMessages(msgs []Message, emitRefs bool, deferred map[string]struct{}) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	loaded := map[string]struct{}{}
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		if m.Assistant != nil {
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": anthropicContent(m.Assistant),
			})
			continue
		}
		if isAnthropicToolResult(m) {
			if !emitRefs {
				tr, _ := convertAnthropicToolResult(m, nil, loaded)
				out = append(out, map[string]any{
					"role":    "user",
					"content": []map[string]any{tr},
				})
				continue
			}
			var blocks []map[string]any
			var siblings []map[string]any
			j := i
			for j < len(msgs) {
				n := msgs[j]
				if n.Assistant != nil || !isAnthropicToolResult(n) {
					break
				}
				tr, sib := convertAnthropicToolResult(n, deferred, loaded)
				blocks = append(blocks, tr)
				siblings = append(siblings, sib...)
				j++
			}
			i = j - 1
			out = append(out, map[string]any{
				"role":    "user",
				"content": append(blocks, siblings...),
			})
			continue
		}
		role := m.Role
		if role == "" {
			role = RoleUser
		}
		out = append(out, map[string]any{"role": role, "content": anthropicUserContent(m)})
	}
	return out
}

func isAnthropicToolResult(m Message) bool {
	return m.Role == RoleToolResult || m.ToolCallID != ""
}

func convertAnthropicToolResult(m Message, deferred map[string]struct{}, loaded map[string]struct{}) (block map[string]any, siblings []map[string]any) {
	inner, siblingBlocks := anthropicToolResultBody(m)
	var refs []map[string]any
	for _, name := range m.AddedToolNames {
		if name == "" {
			continue
		}
		if _, ok := deferred[name]; !ok {
			continue
		}
		if _, ok := loaded[name]; ok {
			continue
		}
		loaded[name] = struct{}{}
		refs = append(refs, map[string]any{"type": "tool_reference", "tool_name": name})
	}
	block = map[string]any{
		"type":        "tool_result",
		"tool_use_id": m.ToolCallID,
		"content":     inner,
	}
	if m.IsError {
		block["is_error"] = true
	}
	if len(refs) == 0 {
		return block, nil
	}
	block["content"] = refs
	return block, siblingBlocks
}

func anthropicToolResultBody(m Message) (inner any, siblings []map[string]any) {
	text, imgs := ParseToolContent(m.Content)
	if len(imgs) == 0 {
		imgs = m.Images
		text = m.Content
	}
	if len(imgs) == 0 {
		sib := []map[string]any{}
		if text != "" {
			sib = append(sib, map[string]any{"type": "text", "text": text})
		}
		return text, sib
	}
	blocks := make([]map[string]any, 0, 1+len(imgs))
	if text != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	for _, img := range imgs {
		blocks = append(blocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": img.MimeType,
				"data":       img.Data,
			},
		})
	}
	return blocks, blocks
}

func anthropicUserContent(m Message) any {
	if len(m.Images) == 0 {
		return m.Content
	}
	blocks := make([]map[string]any, 0, 1+len(m.Images))
	if m.Content != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
	}
	for _, img := range m.Images {
		blocks = append(blocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": img.MimeType,
				"data":       img.Data,
			},
		})
	}
	hasText := false
	for _, b := range blocks {
		if b["type"] == "text" {
			hasText = true
			break
		}
	}
	if !hasText {
		blocks = append([]map[string]any{{"type": "text", "text": "(see attached image)"}}, blocks...)
	}
	return blocks
}

func anthropicContent(msg *AssistantMessage) []map[string]any {
	blocks := make([]map[string]any, 0, len(msg.Content))
	for _, c := range msg.Content {
		switch c.Type {
		case KindText:
			blocks = append(blocks, map[string]any{"type": "text", "text": c.Text})
		case KindThinking:
			if c.Redacted {
				data := c.ThinkingSignature
				if data == "" {
					data = c.Thinking
				}
				blocks = append(blocks, map[string]any{"type": "redacted_thinking", "data": data})
				continue
			}
			b := map[string]any{"type": "thinking", "thinking": c.Thinking}
			if c.ThinkingSignature != "" {
				b["signature"] = c.ThinkingSignature
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

// OpenAIWireMessages maps pigo messages onto OpenAI Chat Completions.
// toolResult becomes role=tool with tool_call_id; assistant tool calls become
// tool_calls.
func OpenAIWireMessages(msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if m.Assistant != nil {
			out = append(out, openaiAssistant(m.Assistant))
			continue
		}
		if m.Role == RoleToolResult || m.ToolCallID != "" {
			text, imgs := ParseToolContent(m.Content)
			if text == "" {
				text = m.Content
			}
			if len(imgs) == 0 {
				imgs = m.Images
			}
			msg := map[string]any{
				"role":         "tool",
				"tool_call_id": m.ToolCallID,
			}
			if len(imgs) == 0 {
				msg["content"] = text
			} else {
				blocks := make([]map[string]any, 0, 1+len(imgs))
				if text != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": text})
				}
				for _, img := range imgs {
					blocks = append(blocks, map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": "data:" + img.MimeType + ";base64," + img.Data,
						},
					})
				}
				msg["content"] = blocks
			}
			out = append(out, msg)
			continue
		}
		role := m.Role
		if role == RoleTool {
			role = "tool"
		}
		out = append(out, map[string]any{"role": role, "content": openaiUserContent(m)})
	}
	return out
}

func openaiUserContent(m Message) any {
	if len(m.Images) == 0 {
		return m.Content
	}
	blocks := make([]map[string]any, 0, 1+len(m.Images))
	blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
	for _, img := range m.Images {
		blocks = append(blocks, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": "data:" + img.MimeType + ";base64," + img.Data,
			},
		})
	}
	return blocks
}

func openaiAssistant(msg *AssistantMessage) map[string]any {
	var text string
	var thinking string
	var toolCalls []map[string]any
	for _, c := range msg.Content {
		switch c.Type {
		case KindText:
			text += c.Text
		case KindThinking:
			thinking += c.Thinking
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
	if thinking != "" {
		out["reasoning_content"] = thinking
	}
	if len(toolCalls) > 0 {
		out["tool_calls"] = toolCalls
	}
	return out
}
