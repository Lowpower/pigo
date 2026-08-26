package agent

import (
	"fmt"

	"github.com/Lowpower/pigo/internal/ai"
)

// ToJSON maps an AgentEvent to the JSON-mode / RPC stdout shape.
//
// message_update drops the cumulative message and any `partial` snapshot, leaving
// usage + assistantMessageEvent. toolcall_start also carries id and toolName.
func ToJSON(ev Event) (any, error) {
	switch ev.Type {
	case EventAgentStart:
		return map[string]any{"type": "agent_start"}, nil
	case EventTurnStart:
		return map[string]any{"type": "turn_start"}, nil
	case EventTurnEnd:
		return map[string]any{
			"type":        "turn_end",
			"message":     assistantJSON(ev.Assistant),
			"toolResults": toolResultsJSON(ev.ToolResults),
		}, nil
	case EventMessageStart, EventMessageEnd:
		return map[string]any{
			"type":    string(ev.Type),
			"message": eventMessageJSON(ev),
		}, nil
	case EventMessageUpdate:
		return messageUpdateJSON(ev)
	case EventToolStart:
		return map[string]any{
			"type":       "tool_execution_start",
			"toolCallId": ev.ToolCallID,
			"toolName":   ev.ToolName,
			"args":       ev.Args,
		}, nil
	case EventToolEnd:
		return map[string]any{
			"type":       "tool_execution_end",
			"toolCallId": ev.ToolCallID,
			"toolName":   ev.ToolName,
			"result":     ev.Result,
			"isError":    ev.IsError,
		}, nil
	case EventAgentEnd:
		msgs := make([]any, 0, len(ev.Messages))
		for _, m := range ev.Messages {
			msgs = append(msgs, msgJSON(m))
		}
		return map[string]any{
			"type":      "agent_end",
			"messages":  msgs,
			"willRetry": ev.WillRetry,
		}, nil
	default:
		return map[string]any{"type": string(ev.Type)}, nil
	}
}

func messageUpdateJSON(ev Event) (any, error) {
	if ev.AIEvent == nil {
		return nil, fmt.Errorf("message_update missing assistantMessageEvent")
	}
	usage := ai.Usage{}
	if ev.Assistant != nil {
		usage = ev.Assistant.Usage
	}
	ame, err := assistantMessageEventJSON(ev.AIEvent)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type":                  "message_update",
		"usage":                 usage,
		"assistantMessageEvent": ame,
	}, nil
}

func assistantMessageEventJSON(ev *ai.Event) (map[string]any, error) {
	out := map[string]any{"type": string(ev.Type)}
	switch ev.Type {
	case ai.EventStart:
		return out, nil
	case ai.EventTextStart, ai.EventThinkingStart:
		out["contentIndex"] = ev.ContentIndex
		return out, nil
	case ai.EventTextDelta, ai.EventThinkingDelta, ai.EventToolCallDelta:
		out["contentIndex"] = ev.ContentIndex
		out["delta"] = ev.Delta
		return out, nil
	case ai.EventTextEnd, ai.EventThinkingEnd:
		out["contentIndex"] = ev.ContentIndex
		out["content"] = ev.Content
		return out, nil
	case ai.EventToolCallStart:
		out["contentIndex"] = ev.ContentIndex
		if ev.Partial != nil && ev.ContentIndex >= 0 && ev.ContentIndex < len(ev.Partial.Content) {
			c := ev.Partial.Content[ev.ContentIndex]
			if c != nil && c.Type == ai.KindToolCall {
				out["id"] = c.ToolID
				out["toolName"] = c.ToolName
			} else {
				return nil, fmt.Errorf("toolcall_start content at index %d is not a tool call", ev.ContentIndex)
			}
		} else {
			return nil, fmt.Errorf("toolcall_start content at index %d is not a tool call", ev.ContentIndex)
		}
		return out, nil
	case ai.EventToolCallEnd:
		out["contentIndex"] = ev.ContentIndex
		out["toolCall"] = ev.ToolCall
		return out, nil
	case ai.EventDone:
		out["reason"] = ev.Reason
		out["message"] = ev.Message
		return out, nil
	case ai.EventError:
		out["reason"] = ev.Reason
		out["error"] = ev.Message
		return out, nil
	default:
		return out, nil
	}
}

func eventMessageJSON(ev Event) any {
	if ev.Msg != nil {
		return msgJSON(*ev.Msg)
	}
	return assistantJSON(ev.Assistant)
}

func assistantJSON(m *ai.AssistantMessage) any {
	if m == nil {
		return nil
	}
	return m
}

func toolResultsJSON(results []Msg) []any {
	out := make([]any, 0, len(results))
	for _, m := range results {
		out = append(out, msgJSON(m))
	}
	return out
}

func msgJSON(m Msg) any {
	switch m.Role {
	case RoleAssistant:
		if m.Assistant != nil {
			return m.Assistant
		}
		return map[string]any{"role": "assistant", "content": m.Text}
	case RoleToolResult:
		return map[string]any{
			"role":       "toolResult",
			"toolCallId": m.ToolCallID,
			"toolName":   m.ToolName,
			"content":    []map[string]any{{"type": "text", "text": m.Text}},
			"isError":    m.IsError,
		}
	default:
		if len(m.Images) == 0 {
			return map[string]any{"role": "user", "content": m.Text}
		}
		return map[string]any{"role": "user", "content": UserContentBlocks(m.Text, m.Images)}
	}
}

// UserContentBlocks is the user-message content array: text block plus images.
func UserContentBlocks(text string, images []ai.ImageContent) []any {
	blocks := []any{map[string]any{"type": "text", "text": text}}
	for _, img := range images {
		typ := img.Type
		if typ == "" {
			typ = "image"
		}
		blocks = append(blocks, map[string]any{
			"type":     typ,
			"data":     img.Data,
			"mimeType": img.MimeType,
		})
	}
	return blocks
}
