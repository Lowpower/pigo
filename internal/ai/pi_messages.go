package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PiMessagesClient posts {model, context, options} to {base}/messages and
// reads SSE events that already use pigo's event type names.
type PiMessagesClient struct {
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
}

// StreamFn returns a StreamFn bound to this client.
func (c *PiMessagesClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		if c.APIKey == "" {
			return errorStreamProvider(opts.Model, "pi-messages", `No API key provided for provider "radius"`), nil
		}
		base := strings.TrimRight(c.BaseURL, "/")
		if base == "" {
			return errorStreamProvider(opts.Model, "pi-messages", "pi-messages requires a base URL"), nil
		}
		payload := map[string]any{
			"model": opts.Model,
			"context": map[string]any{
				"systemPrompt": reqCtx.System,
				"messages":     reqCtx.Messages,
				"tools":        reqCtx.Tools,
			},
			"options": piMessagesOptions(opts),
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/messages", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("content-type", "application/json")
		httpReq.Header.Set("authorization", "Bearer "+c.APIKey)
		httpReq.Header.Set("accept", "text/event-stream")
		for k, v := range c.Headers {
			httpReq.Header.Set(k, v)
		}
		client := c.HTTPClient
		if client == nil {
			client = &http.Client{Timeout: 5 * time.Minute}
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return errorStreamProvider(opts.Model, "pi-messages",
				fmt.Sprintf("%d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))), nil
		}
		s := NewEventStream(16)
		out := &AssistantMessage{
			Role: RoleAssistant, Content: []*Content{}, API: "pi-messages",
			Provider: "radius", Model: opts.Model, StopReason: StopPending,
		}
		go func() {
			defer s.end()
			defer func() { _ = resp.Body.Close() }()
			streamPiMessagesSSE(ctx, resp.Body, out, s)
		}()
		return s, nil
	}
}

func piMessagesOptions(opts Options) map[string]any {
	o := map[string]any{}
	if opts.MaxTokens > 0 {
		o["maxTokens"] = opts.MaxTokens
	}
	if opts.Thinking != "" && opts.Thinking != "off" {
		o["reasoning"] = opts.Thinking
	}
	return o
}

type piMessagesEvent struct {
	Type         string     `json:"type"`
	ContentIndex int        `json:"contentIndex"`
	Delta        string     `json:"delta"`
	Content      string     `json:"content"`
	ID           string     `json:"id"`
	ToolName     string     `json:"toolName"`
	ToolCall     *Content   `json:"toolCall"`
	Reason       StopReason `json:"reason"`
	Usage        *Usage     `json:"usage"`
	ErrorMessage string     `json:"errorMessage"`
	ResponseID   string     `json:"responseId"`
}

func streamPiMessagesSSE(ctx context.Context, r io.Reader, out *AssistantMessage, s *EventStream) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var data strings.Builder
	flush := func() bool {
		if data.Len() == 0 {
			return true
		}
		payload := data.String()
		data.Reset()
		if payload == "[DONE]" {
			return true
		}
		return handlePiMessagesEvent(ctx, payload, out, s)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !flush() {
				return
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimPrefix(rest, " "))
		}
	}
	_ = flush()
	if out.StopReason == StopPending {
		finishError(ctx, out, s, "pi-messages stream ended without a terminal event")
	}
}

func handlePiMessagesEvent(ctx context.Context, payload string, out *AssistantMessage, s *EventStream) bool {
	var ev piMessagesEvent
	if json.Unmarshal([]byte(payload), &ev) != nil {
		return true
	}
	switch ev.Type {
	case "start":
		return s.push(ctx, Event{Type: EventStart, Partial: out})
	case "text_start":
		ensurePiBlock(out, ev.ContentIndex, KindText)
		return s.push(ctx, Event{Type: EventTextStart, ContentIndex: ev.ContentIndex, Partial: out})
	case "text_delta":
		ensurePiBlock(out, ev.ContentIndex, KindText)
		out.Content[ev.ContentIndex].Text += ev.Delta
		return s.push(ctx, Event{Type: EventTextDelta, ContentIndex: ev.ContentIndex, Delta: ev.Delta, Partial: out})
	case "text_end":
		ensurePiBlock(out, ev.ContentIndex, KindText)
		if ev.Content != "" {
			out.Content[ev.ContentIndex].Text = ev.Content
		}
		return s.push(ctx, Event{Type: EventTextEnd, ContentIndex: ev.ContentIndex, Content: out.Content[ev.ContentIndex].Text, Partial: out})
	case "thinking_start":
		ensurePiBlock(out, ev.ContentIndex, KindThinking)
		return s.push(ctx, Event{Type: EventThinkingStart, ContentIndex: ev.ContentIndex, Partial: out})
	case "thinking_delta":
		ensurePiBlock(out, ev.ContentIndex, KindThinking)
		out.Content[ev.ContentIndex].Thinking += ev.Delta
		return s.push(ctx, Event{Type: EventThinkingDelta, ContentIndex: ev.ContentIndex, Delta: ev.Delta, Partial: out})
	case "thinking_end":
		ensurePiBlock(out, ev.ContentIndex, KindThinking)
		if ev.Content != "" {
			out.Content[ev.ContentIndex].Thinking = ev.Content
		}
		return s.push(ctx, Event{Type: EventThinkingEnd, ContentIndex: ev.ContentIndex, Content: out.Content[ev.ContentIndex].Thinking, Partial: out})
	case "toolcall_start":
		ensurePiBlock(out, ev.ContentIndex, KindToolCall)
		out.Content[ev.ContentIndex].ToolID = ev.ID
		out.Content[ev.ContentIndex].ToolName = ev.ToolName
		if out.Content[ev.ContentIndex].Arguments == nil {
			out.Content[ev.ContentIndex].Arguments = map[string]any{}
		}
		return s.push(ctx, Event{Type: EventToolCallStart, ContentIndex: ev.ContentIndex, Partial: out})
	case "toolcall_delta":
		ensurePiBlock(out, ev.ContentIndex, KindToolCall)
		block := out.Content[ev.ContentIndex]
		block.partialJSON += ev.Delta
		block.Arguments = parseStreamingJSON(block.partialJSON)
		return s.push(ctx, Event{Type: EventToolCallDelta, ContentIndex: ev.ContentIndex, Delta: ev.Delta, Partial: out})
	case "toolcall_end":
		ensurePiBlock(out, ev.ContentIndex, KindToolCall)
		block := out.Content[ev.ContentIndex]
		if ev.ToolCall != nil {
			if ev.ToolCall.ToolID != "" {
				block.ToolID = ev.ToolCall.ToolID
			}
			if ev.ToolCall.ToolName != "" {
				block.ToolName = ev.ToolCall.ToolName
			}
			if ev.ToolCall.Arguments != nil {
				block.Arguments = ev.ToolCall.Arguments
			}
		}
		block.partialJSON = ""
		return s.push(ctx, Event{Type: EventToolCallEnd, ContentIndex: ev.ContentIndex, ToolCall: block, Partial: out})
	case "done":
		if ev.Usage != nil {
			out.Usage = *ev.Usage
		}
		if ev.ResponseID != "" {
			out.ResponseID = ev.ResponseID
		}
		out.StopReason = ev.Reason
		if out.StopReason == "" {
			out.StopReason = StopStop
		}
		return s.push(ctx, Event{Type: EventDone, Reason: out.StopReason, Message: out})
	case "error":
		if ev.Usage != nil {
			out.Usage = *ev.Usage
		}
		out.StopReason = ev.Reason
		if out.StopReason == "" {
			out.StopReason = StopError
		}
		out.ErrorMessage = ev.ErrorMessage
		return s.push(ctx, Event{Type: EventError, Reason: out.StopReason, Message: out})
	}
	return true
}

func ensurePiBlock(out *AssistantMessage, idx int, kind ContentKind) {
	for len(out.Content) <= idx {
		out.Content = append(out.Content, &Content{})
	}
	if out.Content[idx] == nil {
		out.Content[idx] = &Content{Type: kind}
		return
	}
	if out.Content[idx].Type == "" {
		out.Content[idx].Type = kind
	}
}
