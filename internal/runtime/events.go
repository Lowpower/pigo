package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/ext"
)

// DispatchEvent sends a lifecycle event to subscribed hosts in load order.
// Each handler sees the payload after earlier modifications. Timeout or a dead
// child is treated as empty continue.
func (e *Engine) DispatchEvent(ctx context.Context, event string, payload map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	out := payload
	for _, h := range e.Hosts {
		if h == nil || !h.Subscribed(event) {
			continue
		}
		res, err := h.QueryEvent(ctx, event, out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pigo: extension %q event %s: %v\n", h.Name(), event, err)
			continue
		}
		out = mergePayload(out, res)
	}
	return out
}

func mergePayload(base, extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return base
	}
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1" || t == "yes"
	default:
		return false
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func imagesPayload(images []ai.ImageContent) any {
	if len(images) == 0 {
		return nil
	}
	out := make([]any, 0, len(images))
	for _, im := range images {
		out = append(out, map[string]any{"type": im.Type, "data": im.Data, "mimeType": im.MimeType})
	}
	return out
}

func imagesFromPayload(v any) []ai.ImageContent {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []ai.ImageContent
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		data, _ := m["data"].(string)
		mime, _ := m["mimeType"].(string)
		if data == "" || mime == "" {
			continue
		}
		typ, _ := m["type"].(string)
		if typ == "" {
			typ = "image"
		}
		out = append(out, ai.ImageContent{Type: typ, Data: data, MimeType: mime})
	}
	return out
}

func asStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case []any:
		var out []string
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func stringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return nil
	}
	out := map[string]string{}
	for k, val := range m {
		if val == nil {
			out[k] = ""
			continue
		}
		out[k] = fmt.Sprint(val)
	}
	return out
}

func headerMap(h http.Header) map[string]string {
	if h == nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

func unclaimedAcross(hosts []*ext.Host, flags []ext.UnknownFlag) []ext.UnknownFlag {
	var leftover []ext.UnknownFlag
	for _, u := range flags {
		claimed := false
		for _, h := range hosts {
			if h != nil && h.ClaimedFlag(u.Name) {
				claimed = true
				break
			}
		}
		if !claimed {
			leftover = append(leftover, u)
		}
	}
	return leftover
}

func (e *Engine) hasEvent(name string) bool {
	for _, h := range e.Hosts {
		if h != nil && h.Subscribed(name) {
			return true
		}
	}
	return false
}

func (e *Engine) onAgentLifecycle(ctx context.Context, ev agent.Event) {
	switch ev.Type {
	case agent.EventAgentStart:
		e.DispatchEvent(ctx, "agent_start", map[string]any{})
	case agent.EventAgentEnd:
		e.DispatchEvent(ctx, "agent_end", map[string]any{})
	case agent.EventTurnStart:
		e.DispatchEvent(ctx, "turn_start", map[string]any{})
	case agent.EventTurnEnd:
		e.DispatchEvent(ctx, "turn_end", map[string]any{})
	case agent.EventMessageStart, agent.EventMessageUpdate:
		e.emitAgentJSONEvent(ctx, ev)
	case agent.EventMessageEnd:
		if ev.Assistant != nil {
			return
		}
		e.emitAgentJSONEvent(ctx, ev)
	case agent.EventToolUpdate:
		e.emitAgentJSONEvent(ctx, ev)
	}
}

func (e *Engine) emitAgentJSONEvent(ctx context.Context, ev agent.Event) {
	name := string(ev.Type)
	if !e.hasEvent(name) {
		return
	}
	raw, err := agent.ToJSON(ev)
	if err != nil {
		return
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return
	}
	payload := make(map[string]any, len(m))
	for k, v := range m {
		if k == "type" {
			continue
		}
		payload[k] = v
	}
	e.DispatchEvent(ctx, name, payload)
}

func (e *Engine) rewriteAssistantMessageEnd(ctx context.Context, m *ai.AssistantMessage) *ai.AssistantMessage {
	if m == nil || !e.hasEvent("message_end") {
		return m
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var asAny any
	if json.Unmarshal(raw, &asAny) != nil {
		return m
	}
	res := e.DispatchEvent(ctx, "message_end", map[string]any{"message": asAny})
	v, ok := res["message"]
	if !ok {
		return m
	}
	b, err := json.Marshal(v)
	if err != nil {
		return m
	}
	var next ai.AssistantMessage
	if json.Unmarshal(b, &next) != nil {
		return m
	}
	if next.Role != "" && next.Role != m.Role {
		return m
	}
	if next.Role == "" {
		next.Role = m.Role
	}
	return &next
}

func (e *Engine) emitContext(ctx context.Context, msgs []ai.Message) []ai.Message {
	if !e.hasEvent("context") {
		return msgs
	}
	raw, err := json.Marshal(msgs)
	if err != nil {
		return msgs
	}
	var asAny any
	if json.Unmarshal(raw, &asAny) != nil {
		return msgs
	}
	res := e.DispatchEvent(ctx, "context", map[string]any{"messages": asAny})
	if v, ok := res["messages"]; ok {
		b, err := json.Marshal(v)
		if err != nil {
			return msgs
		}
		var next []ai.Message
		if json.Unmarshal(b, &next) == nil {
			return next
		}
	}
	return msgs
}

// UnclaimedFlags are leftover CLI flags no extension registered.
func (e *Engine) UnclaimedFlags() []ext.UnknownFlag {
	return unclaimedAcross(e.Hosts, e.Opts.UnknownFlags)
}
