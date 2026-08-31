package runtime

import (
	"context"
	"fmt"
	"os"

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

// UnclaimedFlags are leftover CLI flags no extension registered.
func (e *Engine) UnclaimedFlags() []ext.UnknownFlag {
	return unclaimedAcross(e.Hosts, e.Opts.UnknownFlags)
}
