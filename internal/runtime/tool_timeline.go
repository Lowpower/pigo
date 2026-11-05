package runtime

import (
	"reflect"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/session"
)

// withToolTimeline inserts session tool declarations into an Anthropic
// Messages request. The agent transcript does not carry those system
// messages, so they are joined here, after context hooks. Joining twice is
// a no-op when the markers are already in place.
func (e *Engine) withToolTimeline(req ai.Context, opts ai.Options) ai.Context {
	if e == nil || e.Opts.Session == nil {
		return req
	}
	provider := opts.Provider
	if provider == "" {
		provider = e.Provider
	}
	if models.APIFor(provider, opts.Model) != "anthropic-messages" {
		return req
	}
	markers := session.ToolMarkers(session.ContextEntries(e.Opts.Session))
	if len(markers) == 0 {
		return req
	}
	req.Messages = mergeToolMarkers(markers, req.Messages)
	return req
}

func mergeToolMarkers(markers []session.ToolMarker, live []ai.Message) []ai.Message {
	out := append([]ai.Message(nil), live...)
	shift := 0
	for _, mk := range markers {
		msg := ai.Message{
			Role:         ai.RoleSystem,
			ToolsAdded:   append([]ai.Tool(nil), mk.Added...),
			ToolsRemoved: append([]string(nil), mk.Removed...),
		}
		at := mk.Before + shift
		if at > len(out) {
			at = len(out)
		}
		if at < len(out) && inlineToolMessageEqual(out[at], msg) {
			shift++
			continue
		}
		out = insertAIMessage(out, at, msg)
		shift++
	}
	return out
}

func inlineToolMessageEqual(a, b ai.Message) bool {
	return a.Role == b.Role && a.Content == b.Content &&
		reflect.DeepEqual(a.ToolsAdded, b.ToolsAdded) &&
		reflect.DeepEqual(a.ToolsRemoved, b.ToolsRemoved)
}

func insertAIMessage(msgs []ai.Message, at int, msg ai.Message) []ai.Message {
	if at >= len(msgs) {
		return append(msgs, msg)
	}
	msgs = append(msgs, ai.Message{})
	copy(msgs[at+1:], msgs[at:])
	msgs[at] = msg
	return msgs
}
