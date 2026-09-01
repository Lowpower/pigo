package ext

import (
	"context"
	"encoding/json"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/protocol"
)

// OAuth sends oauth_login / oauth_refresh / oauth_get_api_key and waits for oauth_result.
func (h *Host) OAuth(ctx context.Context, kind string, cred map[string]any) (map[string]any, error) {
	typ := protocol.TypeOAuthLogin
	switch kind {
	case "refresh":
		typ = protocol.TypeOAuthRefresh
	case "get_api_key":
		typ = protocol.TypeOAuthGetAPIKey
	}
	m, err := h.roundTrip(ctx, protocol.Message{Type: typ, Payload: cred}, protocol.TypeOAuthResult)
	if err != nil {
		return nil, err
	}
	if m.Payload == nil {
		return map[string]any{}, nil
	}
	return m.Payload, nil
}

// RefreshModels asks the extension to replace its model list.
func (h *Host) RefreshModels(ctx context.Context) ([]map[string]any, error) {
	m, err := h.roundTrip(ctx, protocol.Message{Type: protocol.TypeRefreshModels}, protocol.TypeRefreshModelsResult)
	if err != nil {
		return nil, err
	}
	if m.Payload == nil {
		return nil, nil
	}
	raw, _ := m.Payload["models"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if mm, ok := item.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out, nil
}

// Stream is the custom StreamFn for a provider registered with stream: true.
func (h *Host) Stream(provider string) ai.StreamFn {
	return func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		id := newID()
		ch := make(chan protocol.Message, 64)
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, errClosed
		}
		h.streamCh[id] = ch
		h.mu.Unlock()

		payload := map[string]any{
			"provider": provider,
			"model":    opts.Model,
			"api":      "",
			"system":   req.System,
			"messages": req.Messages,
			"tools":    req.Tools,
			"options": map[string]any{
				"model":          opts.Model,
				"maxTokens":      opts.MaxTokens,
				"thinking":       opts.Thinking,
				"thinkingBudget": opts.ThinkingBudget,
			},
		}
		if err := h.send(protocol.Message{Type: protocol.TypeStreamStart, ID: id, Payload: payload}); err != nil {
			h.mu.Lock()
			delete(h.streamCh, id)
			h.mu.Unlock()
			return nil, err
		}

		s := ai.NewEventStream(32)
		go func() {
			defer s.Close()
			defer func() {
				h.mu.Lock()
				delete(h.streamCh, id)
				h.mu.Unlock()
			}()
			asm := streamAssembler{msg: &ai.AssistantMessage{Role: ai.RoleAssistant, StopReason: ai.StopPending}}
			for {
				select {
				case <-ctx.Done():
					_ = h.send(protocol.Message{Type: protocol.TypeStreamAbort, ID: id})
					msg := asm.msg
					msg.StopReason = ai.StopAborted
					msg.ErrorMessage = ctx.Err().Error()
					s.Push(ctx, ai.Event{Type: ai.EventError, Reason: ai.StopAborted, Message: msg})
					return
				case m, ok := <-ch:
					if !ok {
						msg := asm.msg
						msg.StopReason = ai.StopAborted
						s.Push(ctx, ai.Event{Type: ai.EventError, Reason: ai.StopAborted, Message: msg})
						return
					}
					ev, terminal := asm.apply(m)
					if !s.Push(ctx, ev) {
						_ = h.send(protocol.Message{Type: protocol.TypeStreamAbort, ID: id})
						return
					}
					if terminal {
						return
					}
				}
			}
		}()
		return s, nil
	}
}

type streamAssembler struct {
	msg *ai.AssistantMessage
}

func (a *streamAssembler) apply(m protocol.Message) (ai.Event, bool) {
	p := m.Payload
	if p == nil {
		p = map[string]any{}
	}
	idx := intFrom(p["contentIndex"])
	delta, _ := p["delta"].(string)
	content, _ := p["content"].(string)
	switch m.Event {
	case "start":
		return ai.Event{Type: ai.EventStart, Partial: a.msg}, false
	case "text_start":
		a.ensure(idx, ai.KindText)
		return ai.Event{Type: ai.EventTextStart, ContentIndex: idx, Partial: a.msg}, false
	case "text_delta":
		a.ensure(idx, ai.KindText)
		a.msg.Content[idx].Text += delta
		return ai.Event{Type: ai.EventTextDelta, ContentIndex: idx, Delta: delta, Partial: a.msg}, false
	case "text_end":
		a.ensure(idx, ai.KindText)
		if content != "" {
			a.msg.Content[idx].Text = content
		}
		return ai.Event{Type: ai.EventTextEnd, ContentIndex: idx, Content: a.msg.Content[idx].Text, Partial: a.msg}, false
	case "thinking_start":
		a.ensure(idx, ai.KindThinking)
		return ai.Event{Type: ai.EventThinkingStart, ContentIndex: idx, Partial: a.msg}, false
	case "thinking_delta":
		a.ensure(idx, ai.KindThinking)
		a.msg.Content[idx].Thinking += delta
		return ai.Event{Type: ai.EventThinkingDelta, ContentIndex: idx, Delta: delta, Partial: a.msg}, false
	case "thinking_end":
		a.ensure(idx, ai.KindThinking)
		if content != "" {
			a.msg.Content[idx].Thinking = content
		}
		return ai.Event{Type: ai.EventThinkingEnd, ContentIndex: idx, Content: a.msg.Content[idx].Thinking, Partial: a.msg}, false
	case "toolcall_start":
		a.ensure(idx, ai.KindToolCall)
		return ai.Event{Type: ai.EventToolCallStart, ContentIndex: idx, Partial: a.msg}, false
	case "toolcall_delta":
		a.ensure(idx, ai.KindToolCall)
		return ai.Event{Type: ai.EventToolCallDelta, ContentIndex: idx, Delta: delta, Partial: a.msg}, false
	case "toolcall_end":
		a.ensure(idx, ai.KindToolCall)
		return ai.Event{Type: ai.EventToolCallEnd, ContentIndex: idx, ToolCall: a.msg.Content[idx], Partial: a.msg}, false
	case "done":
		final := messageFromPayload(p["message"], a.msg)
		if final.StopReason == "" || final.StopReason == ai.StopPending {
			final.StopReason = ai.StopStop
		}
		return ai.Event{Type: ai.EventDone, Reason: final.StopReason, Message: final}, true
	case "error":
		final := messageFromPayload(p["message"], a.msg)
		reason := ai.StopError
		if r, _ := p["reason"].(string); r == "aborted" {
			reason = ai.StopAborted
		}
		final.StopReason = reason
		if final.ErrorMessage == "" {
			final.ErrorMessage, _ = p["reason"].(string)
		}
		return ai.Event{Type: ai.EventError, Reason: reason, Message: final}, true
	default:
		return ai.Event{Type: ai.EventStart, Partial: a.msg}, false
	}
}

func (a *streamAssembler) ensure(idx int, kind ai.ContentKind) {
	for len(a.msg.Content) <= idx {
		a.msg.Content = append(a.msg.Content, &ai.Content{Type: kind})
	}
	if a.msg.Content[idx] == nil {
		a.msg.Content[idx] = &ai.Content{Type: kind}
	}
	if a.msg.Content[idx].Type == "" {
		a.msg.Content[idx].Type = kind
	}
}

func messageFromPayload(v any, fallback *ai.AssistantMessage) *ai.AssistantMessage {
	if v == nil {
		return fallback
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fallback
	}
	var msg ai.AssistantMessage
	if json.Unmarshal(b, &msg) != nil || msg.Role == "" {
		return fallback
	}
	return &msg
}

func intFrom(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return 0
	}
}
