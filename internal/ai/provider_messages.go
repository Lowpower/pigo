package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AnthropicWireMessages maps pigo messages onto the Anthropic Messages API
// shape. Assistant tool calls become tool_use blocks; toolResult becomes a user
// message with tool_result.
func AnthropicWireMessages(msgs []Message) []map[string]any {
	return anthropicWireMessages(msgs, false, nil, nil, "", false)
}

func anthropicWireMessages(msgs []Message, _ bool, deferred map[string]struct{}, levels map[int]string, provider string, allowEmptySignature bool) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	loaded := map[string]struct{}{}
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		if m.Assistant != nil {
			blocks := anthropicContent(m.Assistant, allowEmptySignature)
			if len(blocks) == 0 {
				continue
			}
			if levels != nil {
				if effort, ok := replayMidConvoEffort(m.Assistant, provider); ok {
					levels[len(out)] = effort
				}
			}
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": blocks,
			})
			continue
		}
		if isAnthropicToolResult(m) {
			var blocks []map[string]any
			var siblings []map[string]any
			var imageSiblings []map[string]any
			j := i
			for j < len(msgs) {
				n := msgs[j]
				if n.Assistant != nil || !isAnthropicToolResult(n) {
					break
				}
				n, images := splitErrorToolResultImages(n)
				tr, sib := convertAnthropicToolResult(n, deferred, loaded)
				blocks = append(blocks, tr)
				siblings = append(siblings, sib...)
				if len(images) > 0 {
					imageSiblings = append(imageSiblings, map[string]any{"type": "text", "text": errorToolImagesCaption(n)})
					for _, img := range images {
						imageSiblings = append(imageSiblings, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": img.MimeType, "data": img.Data}})
					}
				}
				j++
			}
			i = j - 1
			out = append(out, map[string]any{
				"role":    "user",
				"content": append(blocks, siblings...),
			})
			if len(imageSiblings) > 0 {
				out = append(out, map[string]any{"role": "user", "content": imageSiblings})
			}
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

const anthropicErrorImagePlaceholder = "(image attached after the tool results)"

func splitErrorToolResultImages(m Message) (Message, []ImageContent) {
	if !m.IsError {
		return m, nil
	}
	blocks, ok := toolContentBlocks(m)
	if !ok {
		return m, nil
	}
	var textBlocks []toolContentBlock
	var images []ImageContent
	previousPlaceholder := false
	for _, block := range blocks {
		if block.Type == "image" {
			if block.Data != "" {
				images = append(images, ImageContent{Type: "image", Data: block.Data, MimeType: block.MimeType})
			}
			if !previousPlaceholder {
				textBlocks = append(textBlocks, toolContentBlock{Type: "text", Text: anthropicErrorImagePlaceholder})
			}
			previousPlaceholder = true
			continue
		}
		textBlocks = append(textBlocks, block)
		previousPlaceholder = block.Type == "text" && block.Text == anthropicErrorImagePlaceholder
	}
	if len(images) == 0 {
		return m, nil
	}
	texts := make([]string, 0, len(textBlocks))
	for _, block := range textBlocks {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	m.Content = strings.Join(texts, "\n")
	m.Images = nil
	return m, images
}

func errorToolImagesCaption(m Message) string {
	return fmt.Sprintf("Images from the error result of tool call %s (%s):", m.ToolCallID, m.ToolName)
}

func toolContentBlocks(m Message) ([]toolContentBlock, bool) {
	raw := strings.TrimSpace(m.Content)
	if raw != "" {
		var obj struct {
			Content []toolContentBlock `json:"content"`
		}
		if json.Unmarshal([]byte(raw), &obj) == nil && len(obj.Content) > 0 {
			return appendFallbackImages(obj.Content, m.Images), true
		}
		var arr []toolContentBlock
		if json.Unmarshal([]byte(raw), &arr) == nil && len(arr) > 0 {
			return appendFallbackImages(arr, m.Images), true
		}
	}
	if len(m.Images) == 0 {
		return nil, false
	}
	blocks := []toolContentBlock{}
	if m.Content != "" {
		blocks = append(blocks, toolContentBlock{Type: "text", Text: m.Content})
	}
	for _, img := range m.Images {
		blocks = append(blocks, toolContentBlock{Type: "image", Data: img.Data, MimeType: img.MimeType})
	}
	return blocks, true
}

func appendFallbackImages(blocks []toolContentBlock, images []ImageContent) []toolContentBlock {
	for _, block := range blocks {
		if block.Type == "image" && block.Data != "" {
			return blocks
		}
	}
	for _, img := range images {
		blocks = append(blocks, toolContentBlock{Type: "image", Data: img.Data, MimeType: img.MimeType})
	}
	return blocks
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

func anthropicContent(msg *AssistantMessage, allowEmptySignature bool) []map[string]any {
	blocks := make([]map[string]any, 0, len(msg.Content))
	for _, c := range msg.Content {
		switch c.Type {
		case KindText:
			if strings.TrimSpace(c.Text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": c.Text})
		case KindThinking:
			if c.Redacted {
				data := c.ThinkingSignature
				if data == "" {
					data = c.Thinking
				}
				if strings.TrimSpace(data) == "" {
					continue
				}
				blocks = append(blocks, map[string]any{"type": "redacted_thinking", "data": data})
				continue
			}
			hasSig := strings.TrimSpace(c.ThinkingSignature) != ""
			if strings.TrimSpace(c.Thinking) == "" && !hasSig {
				continue
			}
			if !hasSig {
				if allowEmptySignature {
					blocks = append(blocks, map[string]any{
						"type":      "thinking",
						"thinking":  c.Thinking,
						"signature": "",
					})
				} else {
					blocks = append(blocks, map[string]any{"type": "text", "text": c.Thinking})
				}
				continue
			}
			blocks = append(blocks, map[string]any{
				"type":      "thinking",
				"thinking":  c.Thinking,
				"signature": c.ThinkingSignature,
			})
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
	if len(blocks) == 0 && strings.TrimSpace(msg.Text()) != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": msg.Text()})
	}
	return blocks
}

type openAIWireOpts struct {
	toolResultName     bool
	assistantAfterTool bool
	thinkingAsText     bool
	emptyReasoning     bool
}

// OpenAIWireMessages maps pigo messages onto OpenAI Chat Completions.
// toolResult becomes role=tool with tool_call_id; assistant tool calls become
// tool_calls.
func OpenAIWireMessages(msgs []Message) []map[string]any {
	return openAIWireMessages(msgs, nil, openAIWireOpts{}, Options{})
}

func chatToolDef(t Tool, opts Options) map[string]any {
	parameters := t.Parameters
	strict := sendChatStrict(t, opts)
	if strict {
		parameters = strictToolParameters(t.Parameters)
	}
	fn := map[string]any{"name": t.Name, "description": t.Description, "parameters": parameters}
	if strict {
		fn["strict"] = true
	}
	return map[string]any{"type": "function", "function": fn}
}

func kimiToolsMessage(m Message, deferred []Tool, loaded map[string]struct{}, opts Options) map[string]any {
	tools := takeDeferredTools(m.AddedToolNames, deferred, loaded)
	if len(tools) == 0 {
		return nil
	}
	defs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, chatToolDef(t, opts))
	}
	return map[string]any{"role": "system", "tools": defs}
}

func openAIWireMessages(msgs []Message, deferred []Tool, w openAIWireOpts, opts Options) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	loaded := map[string]struct{}{}
	lastRole := ""
	for _, m := range msgs {
		if w.assistantAfterTool && lastRole == "tool" && m.Role == RoleUser && m.Assistant == nil && m.ToolCallID == "" {
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": "I have processed the tool results.",
			})
		}
		if m.Assistant != nil {
			out = append(out, openaiAssistant(m.Assistant, w))
			lastRole = "assistant"
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
			if w.toolResultName && m.ToolName != "" {
				msg["name"] = m.ToolName
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
			lastRole = "tool"
			if extra := kimiToolsMessage(m, deferred, loaded, opts); extra != nil {
				out = append(out, extra)
				lastRole = "system"
			}
			continue
		}
		role := m.Role
		if role == RoleTool {
			role = "tool"
		}
		out = append(out, map[string]any{"role": role, "content": openaiUserContent(m)})
		lastRole = role
	}
	return out
}

func openaiUserContent(m Message) any {
	if len(m.Images) == 0 {
		return m.Content
	}
	blocks := make([]map[string]any, 0, 1+len(m.Images))
	if m.Content != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
	}
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

func openaiAssistant(msg *AssistantMessage, w openAIWireOpts) map[string]any {
	var text string
	var thinking string
	var thoughts []string
	var texts []string
	var toolCalls []map[string]any
	for _, c := range msg.Content {
		switch c.Type {
		case KindText:
			text += c.Text
			if strings.TrimSpace(c.Text) != "" {
				texts = append(texts, c.Text)
			}
		case KindThinking:
			thinking += c.Thinking
			if strings.TrimSpace(c.Thinking) != "" {
				thoughts = append(thoughts, c.Thinking)
			}
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
	if w.thinkingAsText && len(thoughts) > 0 {
		parts := make([]map[string]any, 0, 1+len(texts))
		parts = append(parts, map[string]any{"type": "text", "text": strings.Join(thoughts, "\n\n")})
		for _, part := range texts {
			parts = append(parts, map[string]any{"type": "text", "text": part})
		}
		out["content"] = parts
	} else if thinking != "" {
		out["reasoning_content"] = thinking
	}
	if len(toolCalls) > 0 {
		out["tool_calls"] = toolCalls
	}
	if details := openAIReasoningDetails(msg); len(details) > 0 {
		out["reasoning_details"] = details
	}
	if w.emptyReasoning {
		if _, ok := out["reasoning_content"]; !ok {
			out["reasoning_content"] = ""
		}
	}
	return out
}

func openAIReasoningDetails(msg *AssistantMessage) []any {
	var out []any
	for _, c := range msg.Content {
		if c == nil || c.Type != KindThinking {
			continue
		}
		for _, detail := range parseOpenAIReasoningDetails(c.ThinkingSignature) {
			out = append(out, detail)
		}
	}
	return out
}
