package ext

import (
	"context"
	"encoding/json"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/protocol"
)

type (
	callIDKey  struct{}
	outcomeKey struct{}
)

// ToolResult is the outcome of one extension tool call.
type ToolResult struct {
	Text       string
	IsError    bool
	Structured json.RawMessage
	Usage      *ai.Usage
}

// WithToolCallID attaches the model-issued tool call id to ctx.
func WithToolCallID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, callIDKey{}, id)
}

// ToolCallID returns the model-issued id stored by WithToolCallID.
func ToolCallID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(callIDKey{}).(string)
	return id
}

// WithOutcome attaches a box that a tool execution fills with structured
// content and usage.
func WithOutcome(ctx context.Context, box *ToolResult) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if box == nil {
		return ctx
	}
	return context.WithValue(ctx, outcomeKey{}, box)
}

// OutcomeBox returns the box stored by WithOutcome.
func OutcomeBox(ctx context.Context) *ToolResult {
	if ctx == nil {
		return nil
	}
	box, _ := ctx.Value(outcomeKey{}).(*ToolResult)
	return box
}

func toolResultFromMessage(m protocol.Message) ToolResult {
	return ToolResult{
		Text:       m.Result,
		IsError:    m.IsError,
		Structured: append(json.RawMessage(nil), m.StructuredContent...),
		Usage:      usageFromMap(m.Usage),
	}
}

func usageFromMap(m map[string]any) *ai.Usage {
	if len(m) == 0 {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	var u ai.Usage
	if json.Unmarshal(raw, &u) != nil {
		return nil
	}
	return &u
}

func usageToMap(u *ai.Usage) map[string]any {
	if u == nil {
		return nil
	}
	raw, err := json.Marshal(u)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func stringSliceAny(v any) []string {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
