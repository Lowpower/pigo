package session

import (
	"encoding/json"
	"fmt"

	"github.com/Lowpower/pigo/internal/ai"
)

// JSONValue is a JSON field that preserves an explicit null.
// A nil pointer omits the field; a pointer whose Raw is null or empty writes null.
type JSONValue struct {
	Raw json.RawMessage
}

// MarshalJSON writes null when Raw is empty or JSON null.
func (v JSONValue) MarshalJSON() ([]byte, error) {
	if len(v.Raw) == 0 || string(v.Raw) == "null" {
		return []byte("null"), nil
	}
	return append(json.RawMessage(nil), v.Raw...), nil
}

// UnmarshalJSON keeps the raw JSON, including null.
func (v *JSONValue) UnmarshalJSON(b []byte) error {
	if v == nil {
		return fmt.Errorf("nil JSONValue")
	}
	v.Raw = append(json.RawMessage(nil), b...)
	return nil
}

func nullJSON() *JSONValue {
	return &JSONValue{Raw: json.RawMessage("null")}
}

func isNullJSON(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

// AppendContextEdit records an append-only edit of an earlier context entry.
// A nil replacement omits the target from later model context. The target entry
// itself is not modified.
func (m *Manager) AppendContextEdit(targetID string, replacement json.RawMessage) (*Entry, error) {
	target, ok := m.EntryByID(targetID)
	if !ok {
		return nil, fmt.Errorf("entry %s not found", targetID)
	}
	if !contextEditTarget(target) {
		return nil, fmt.Errorf("entry %s cannot be a context edit target", targetID)
	}
	repl := nullJSON()
	if !isNullJSON(replacement) {
		repl = &JSONValue{Raw: append(json.RawMessage(nil), replacement...)}
	}
	e := &Entry{
		Type:        "context_edit",
		ID:          newUUID(),
		Timestamp:   isoNow(),
		TargetID:    targetID,
		Replacement: repl,
	}
	return m.appendEntry(e)
}

func contextEditTarget(e Entry) bool {
	switch e.Type {
	case "custom_message":
		return true
	case "message", "":
		switch entryRole(&e) {
		case "user", "assistant", "toolResult", "tool":
			return true
		}
	}
	return false
}

// ModelMessages is the provider-facing transcript. context_edit entries on the
// path change this view only; RestoreAIMessages stays the raw transcript.
func ModelMessages(entries []Entry) []ai.Message {
	latest := map[string]json.RawMessage{}
	for _, e := range entries {
		if e.Type != "context_edit" || e.TargetID == "" || e.Replacement == nil {
			continue
		}
		latest[e.TargetID] = e.Replacement.Raw
	}
	out := make([]ai.Message, 0, len(entries))
	for _, e := range entries {
		if e.Type == "context_edit" {
			continue
		}
		repl, ok := latest[e.ID]
		if ok && isNullJSON(repl) {
			continue
		}
		msgs := RestoreAIMessages([]Entry{e})
		if ok {
			for i := range msgs {
				msgs[i] = applyContextReplacement(msgs[i], repl)
			}
		}
		out = append(out, msgs...)
	}
	return out
}

func applyContextReplacement(msg ai.Message, raw json.RawMessage) ai.Message {
	if s, ok := jsonString(raw); ok {
		if msg.Assistant != nil || msg.Role == ai.RoleAssistant {
			return replaceAssistantText(msg, s)
		}
		if msg.Role == ai.RoleToolResult || msg.Role == ai.RoleTool || msg.ToolCallID != "" {
			msg.Content = s
			return msg
		}
		msg.Content = s
		msg.Images = nil
		return msg
	}
	if msg.Assistant != nil || msg.Role == ai.RoleAssistant {
		var blocks []*ai.Content
		if json.Unmarshal(raw, &blocks) == nil && len(blocks) > 0 {
			return replaceAssistantBlocks(msg, blocks)
		}
	}
	if text, images := contentFromRaw(raw); text != "" || len(images) > 0 {
		msg.Content = text
		msg.Images = images
	}
	return msg
}

func replaceAssistantText(msg ai.Message, text string) ai.Message {
	return replaceAssistantBlocks(msg, []*ai.Content{{Type: ai.KindText, Text: text}})
}

func replaceAssistantBlocks(msg ai.Message, blocks []*ai.Content) ai.Message {
	base := msg.Assistant
	if base == nil {
		base = &ai.AssistantMessage{Role: ai.RoleAssistant}
	}
	cp := *base
	cp.Content = blocks
	msg.Assistant = &cp
	msg.Role = ai.RoleAssistant
	msg.Content = cp.Text()
	return msg
}

func jsonString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func contentFromRaw(raw json.RawMessage) (string, []ai.ImageContent) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "", nil
	}
	return parseUserContent(v)
}
