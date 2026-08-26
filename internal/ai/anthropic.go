package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// This file maps Anthropic Messages SSE events to AssistantMessageEvents.

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	anthropicVersion        = "2023-06-01"
	defaultMaxTokens        = 1024
)

// AnthropicClient talks to the Anthropic Messages API (or any compatible gateway).
// BaseURL and APIKey are configurable so a proxy/gateway (e.g. an opencode plan
// endpoint) can be used by setting ANTHROPIC_BASE_URL / ANTHROPIC_API_KEY.
type AnthropicClient struct {
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
}

// NewAnthropicFromEnv builds a client from ANTHROPIC_API_KEY (required) and
// ANTHROPIC_BASE_URL (optional). ok is false when no API key is configured.
func NewAnthropicFromEnv() (*AnthropicClient, bool) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, false
	}
	base := os.Getenv("ANTHROPIC_BASE_URL")
	if base == "" {
		base = defaultAnthropicBaseURL
	}
	return &AnthropicClient{
		BaseURL:    strings.TrimRight(base, "/"),
		APIKey:     key,
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
	}, true
}

// StreamFn returns a StreamFn bound to this client.
func (c *AnthropicClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		body, err := buildAnthropicRequest(reqCtx, opts)
		if err != nil {
			return nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("content-type", "application/json")
		if c.APIKey != "" {
			httpReq.Header.Set("x-api-key", c.APIKey)
		}
		httpReq.Header.Set("anthropic-version", anthropicVersion)
		httpReq.Header.Set("accept", "text/event-stream")
		for k, v := range c.Headers {
			httpReq.Header.Set(k, v)
		}

		client := c.HTTPClient
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, err
		}

		model := opts.Model
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return errorStream(model, fmt.Sprintf("anthropic API error %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))), nil
		}

		s := NewEventStream(16)
		out := newOutputMessage(model)
		go func() {
			defer s.end()
			defer func() { _ = resp.Body.Close() }()
			streamAnthropicSSE(ctx, resp.Body, out, s)
		}()
		return s, nil
	}
}

// StreamAnthropicReader runs the SSE→event core against an already-open reader
// (used for offline fixture tests, and reusable by the HTTP client).
func StreamAnthropicReader(ctx context.Context, r io.Reader, model string) *EventStream {
	s := NewEventStream(16)
	out := newOutputMessage(model)
	go func() {
		defer s.end()
		streamAnthropicSSE(ctx, r, out, s)
	}()
	return s
}

func newOutputMessage(model string) *AssistantMessage {
	return &AssistantMessage{
		Role:       RoleAssistant,
		Content:    []*Content{},
		API:        "anthropic-messages",
		Provider:   "anthropic",
		Model:      model,
		StopReason: StopPending,
	}
}

func errorStream(model, msg string) *EventStream {
	s := NewEventStream(1)
	out := newOutputMessage(model)
	out.StopReason = StopError
	out.ErrorMessage = msg
	s.ch <- Event{Type: EventError, Reason: StopError, Message: out}
	close(s.ch)
	return s
}

func buildAnthropicRequest(reqCtx Context, opts Options) ([]byte, error) {
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	msgs := AnthropicWireMessages(reqCtx.Messages)

	req := map[string]any{
		"model":      opts.Model,
		"max_tokens": maxTokens,
		"messages":   msgs,
		"stream":     true,
	}
	if reqCtx.System != "" {
		req["system"] = reqCtx.System
	}
	if len(reqCtx.Tools) > 0 {
		tools := make([]map[string]any, 0, len(reqCtx.Tools))
		for _, t := range reqCtx.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.Parameters,
			})
		}
		req["tools"] = tools
	}
	if budget := thinkingBudgetTokens(opts.Thinking); budget > 0 {
		req["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		if maxTokens <= budget {
			req["max_tokens"] = budget + 4096
		}
	}
	return json.Marshal(req)
}

func thinkingBudgetTokens(level string) int {
	switch level {
	case "minimal":
		return 1024
	case "low":
		return 2048
	case "medium":
		return 5120
	case "high":
		return 10000
	case "xhigh", "max":
		return 31999
	default:
		return 0
	}
}

// --- SSE core ---

type anthropicUsage struct {
	InputTokens              *int `json:"input_tokens"`
	OutputTokens             *int `json:"output_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
}

type anthropicSSE struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		ID    string         `json:"id"`
		Model string         `json:"model"`
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type      string         `json:"type"`
		Text      string         `json:"text"`
		Thinking  string         `json:"thinking"`
		Signature string         `json:"signature"`
		Data      string         `json:"data"`
		ID        string         `json:"id"`
		Name      string         `json:"name"`
		Input     map[string]any `json:"input"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *anthropicUsage `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// streamAnthropicSSE reads Anthropic SSE from r and pushes AssistantMessageEvents
// onto s, building out in place. It does not close s (the caller does).
func streamAnthropicSSE(ctx context.Context, r io.Reader, out *AssistantMessage, s *EventStream) {
	if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
		return
	}

	pos := map[int]int{} // anthropic block index -> position in out.Content

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var data strings.Builder
	flush := func() bool {
		if data.Len() == 0 {
			return true
		}
		payload := data.String()
		data.Reset()
		return handleAnthropicEvent(ctx, payload, out, pos, s)
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
	if !flush() {
		return
	}

	if err := scanner.Err(); err != nil {
		finishError(ctx, out, s, err.Error())
		return
	}
	switch out.StopReason {
	case StopPending:
		finishError(ctx, out, s, "Anthropic stream ended without a stop reason")
	case StopError, StopAborted:
		finishError(ctx, out, s, out.ErrorMessage)
	default:
		s.push(ctx, Event{Type: EventDone, Reason: out.StopReason, Message: out})
	}
}

func handleAnthropicEvent(ctx context.Context, payload string, out *AssistantMessage, pos map[int]int, s *EventStream) bool {
	var ev anthropicSSE
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return true // ignore unparseable keepalive/comment payloads
	}

	switch ev.Type {
	case "message_start":
		if ev.Message != nil {
			if ev.Message.ID != "" {
				out.ResponseID = ev.Message.ID
			}
			if ev.Message.Model != "" {
				out.Model = ev.Message.Model
			}
			u := ev.Message.Usage
			out.Usage.Input = deref(u.InputTokens)
			out.Usage.Output = deref(u.OutputTokens)
			out.Usage.CacheRead = deref(u.CacheReadInputTokens)
			out.Usage.CacheWrite = deref(u.CacheCreationInputTokens)
			out.Usage.TotalTokens = out.Usage.Input + out.Usage.Output + out.Usage.CacheRead + out.Usage.CacheWrite
		}

	case "content_block_start":
		cb := ev.ContentBlock
		if cb == nil {
			return true
		}
		var block *Content
		switch cb.Type {
		case "text":
			block = &Content{Type: KindText, Text: cb.Text}
		case "thinking":
			block = &Content{Type: KindThinking, Thinking: cb.Thinking, ThinkingSignature: cb.Signature}
		case "redacted_thinking":
			block = &Content{Type: KindThinking, Thinking: "[Reasoning redacted]", ThinkingSignature: cb.Data, Redacted: true}
		case "tool_use":
			args := cb.Input
			if args == nil {
				args = map[string]any{}
			}
			block = &Content{Type: KindToolCall, ToolID: cb.ID, ToolName: cb.Name, Arguments: args}
		default:
			return true
		}
		out.Content = append(out.Content, block)
		idx := len(out.Content) - 1
		pos[ev.Index] = idx
		return s.push(ctx, Event{Type: startEventFor(block.Type), ContentIndex: idx, Partial: out})

	case "content_block_delta":
		d := ev.Delta
		if d == nil {
			return true
		}
		idx, ok := pos[ev.Index]
		if !ok {
			return true
		}
		block := out.Content[idx]
		switch d.Type {
		case "text_delta":
			if block.Type == KindText {
				block.Text += d.Text
				return s.push(ctx, Event{Type: EventTextDelta, ContentIndex: idx, Delta: d.Text, Partial: out})
			}
		case "thinking_delta":
			if block.Type == KindThinking {
				block.Thinking += d.Thinking
				return s.push(ctx, Event{Type: EventThinkingDelta, ContentIndex: idx, Delta: d.Thinking, Partial: out})
			}
		case "input_json_delta":
			if block.Type == KindToolCall {
				block.partialJSON += d.PartialJSON
				block.Arguments = parseStreamingJSON(block.partialJSON)
				return s.push(ctx, Event{Type: EventToolCallDelta, ContentIndex: idx, Delta: d.PartialJSON, Partial: out})
			}
		case "signature_delta":
			if block.Type == KindThinking {
				block.ThinkingSignature += d.Signature
			}
		}

	case "content_block_stop":
		idx, ok := pos[ev.Index]
		if !ok {
			return true
		}
		block := out.Content[idx]
		switch block.Type {
		case KindText:
			return s.push(ctx, Event{Type: EventTextEnd, ContentIndex: idx, Content: block.Text, Partial: out})
		case KindThinking:
			return s.push(ctx, Event{Type: EventThinkingEnd, ContentIndex: idx, Content: block.Thinking, Partial: out})
		case KindToolCall:
			block.Arguments = parseStreamingJSON(block.partialJSON)
			block.partialJSON = ""
			return s.push(ctx, Event{Type: EventToolCallEnd, ContentIndex: idx, ToolCall: block, Partial: out})
		}

	case "message_delta":
		if ev.Delta != nil && ev.Delta.StopReason != "" {
			out.RawStopReason = ev.Delta.StopReason
			out.StopReason = mapAnthropicStopReason(ev.Delta.StopReason)
		}
		if ev.Usage != nil {
			u := ev.Usage
			if u.OutputTokens != nil {
				out.Usage.Output = *u.OutputTokens
			}
			if u.InputTokens != nil {
				out.Usage.Input = *u.InputTokens
			}
			if u.CacheReadInputTokens != nil {
				out.Usage.CacheRead = *u.CacheReadInputTokens
			}
			if u.CacheCreationInputTokens != nil {
				out.Usage.CacheWrite = *u.CacheCreationInputTokens
			}
			out.Usage.TotalTokens = out.Usage.Input + out.Usage.Output + out.Usage.CacheRead + out.Usage.CacheWrite
		}

	case "error":
		if ev.Error != nil {
			out.StopReason = StopError
			out.ErrorMessage = ev.Error.Message
		}
	}

	return true
}

func startEventFor(k ContentKind) EventType {
	switch k {
	case KindText:
		return EventTextStart
	case KindThinking:
		return EventThinkingStart
	default:
		return EventToolCallStart
	}
}

func finishError(ctx context.Context, out *AssistantMessage, s *EventStream, msg string) {
	if out.StopReason != StopAborted {
		out.StopReason = StopError
	}
	if out.ErrorMessage == "" {
		out.ErrorMessage = msg
	}
	s.push(ctx, Event{Type: EventError, Reason: out.StopReason, Message: out})
}

// mapAnthropicStopReason maps Anthropic stop reasons onto StopReason.
func mapAnthropicStopReason(raw string) StopReason {
	switch raw {
	case "end_turn", "stop_sequence", "pause_turn":
		return StopStop
	case "max_tokens":
		return StopLength
	case "tool_use":
		return StopToolUse
	default:
		return StopStop
	}
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
