package ai

import (
	"encoding/json"
	"strings"

	"github.com/Lowpower/pigo/internal/models"
)

const (
	userImagePlaceholder = "(image omitted: model does not support images)"
	toolImagePlaceholder = "(tool image omitted: model does not support images)"
	noResultProvided     = "No result provided"
)

// sanitizeProviderMessages returns a provider-facing copy of msgs.
// Assistant messages with stopReason error or aborted are dropped. Tool calls
// that never received a result get an isError "No result provided" result
// before the next user message, the next assistant message, and the end of
// the history. System messages that land inside an open tool batch are held
// and emitted after that batch's results. When supportsImages is false, user
// and tool-result images are replaced with a single placeholder per run.
// The input messages are not modified.
func sanitizeProviderMessages(msgs []Message, supportsImages bool) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	if !supportsImages {
		msgs = downgradeUnsupportedImages(msgs)
	}
	return closeOrphanToolCalls(msgs)
}

func modelSupportsImages(provider, model string) bool {
	m, _ := models.Lookup(provider, model)
	return m.SupportsImage()
}

func downgradeUnsupportedImages(msgs []Message) []Message {
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		out[i] = downgradeMessageImages(m)
	}
	return out
}

func downgradeMessageImages(m Message) Message {
	if m.Assistant != nil || m.Role == RoleSystem {
		return m
	}
	if isReplayToolResult(m) {
		return downgradeToolImages(m)
	}
	if len(m.Images) == 0 {
		return m
	}
	m.Images = nil
	m.Content = appendPlaceholder(m.Content, userImagePlaceholder)
	return m
}

func downgradeToolImages(m Message) Message {
	if rewritten, ok := rewriteToolImageContent(m.Content); ok {
		m.Content = rewritten
		m.Images = nil
		return m
	}
	if len(m.Images) == 0 {
		return m
	}
	m.Images = nil
	m.Content = appendPlaceholder(m.Content, toolImagePlaceholder)
	return m
}

func appendPlaceholder(content, placeholder string) string {
	if content == "" {
		return placeholder
	}
	return content + "\n" + placeholder
}

func rewriteToolImageContent(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	var obj struct {
		Content []toolContentBlock `json:"content"`
	}
	if json.Unmarshal([]byte(raw), &obj) == nil && len(obj.Content) > 0 {
		next, changed := collapseImageBlocks(obj.Content)
		if !changed {
			return "", false
		}
		obj.Content = next
		b, err := json.Marshal(obj)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
	var arr []toolContentBlock
	if json.Unmarshal([]byte(raw), &arr) == nil && len(arr) > 0 {
		next, changed := collapseImageBlocks(arr)
		if !changed {
			return "", false
		}
		b, err := json.Marshal(next)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
	return "", false
}

func collapseImageBlocks(blocks []toolContentBlock) ([]toolContentBlock, bool) {
	out := make([]toolContentBlock, 0, len(blocks))
	changed := false
	prevPlaceholder := false
	for _, b := range blocks {
		if b.Type == "image" {
			changed = true
			if !prevPlaceholder {
				out = append(out, toolContentBlock{Type: "text", Text: toolImagePlaceholder})
			}
			prevPlaceholder = true
			continue
		}
		out = append(out, b)
		prevPlaceholder = b.Type == "text" && b.Text == toolImagePlaceholder
	}
	return out, changed
}

func isReplayToolResult(m Message) bool {
	return m.Role == RoleToolResult || m.Role == RoleTool || m.ToolCallID != ""
}

func closeOrphanToolCalls(msgs []Message) []Message {
	result := make([]Message, 0, len(msgs))
	var pending []*Content
	existing := map[string]struct{}{}
	var held []Message
	closePending := func() {
		for _, tc := range pending {
			if tc == nil {
				continue
			}
			if _, ok := existing[tc.ToolID]; ok {
				continue
			}
			result = append(result, Message{
				Role:       RoleToolResult,
				ToolCallID: tc.ToolID,
				ToolName:   tc.ToolName,
				Content:    noResultProvided,
				IsError:    true,
			})
		}
		pending = nil
		existing = map[string]struct{}{}
		result = append(result, held...)
		held = nil
	}
	for _, m := range msgs {
		switch {
		case m.Assistant != nil:
			closePending()
			if m.Assistant.StopReason == StopError || m.Assistant.StopReason == StopAborted {
				continue
			}
			if calls := m.Assistant.ToolCalls(); len(calls) > 0 {
				pending = calls
				existing = map[string]struct{}{}
			}
			result = append(result, m)
		case isReplayToolResult(m):
			existing[m.ToolCallID] = struct{}{}
			result = append(result, m)
		case m.Role == RoleSystem:
			if len(pending) > 0 {
				held = append(held, m)
			} else {
				result = append(result, m)
			}
		case m.Role == RoleUser || m.Role == "":
			closePending()
			result = append(result, m)
		default:
			result = append(result, m)
		}
	}
	closePending()
	return result
}
