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

// This file ports the streaming core of pi's packages/ai/src/api/openai-completions.ts
// (the OpenAI Chat Completions SSE format). It covers text and tool-call streaming,
// finish_reason mapping, and usage; the large per-provider compat matrix is not
// ported (only the fields needed by OpenAI-compatible gateways such as opencode).

// OpenAICompletionsClient talks to an OpenAI-compatible /v1/chat/completions
// endpoint. BaseURL and APIKey are configurable; auth is Bearer.
type OpenAICompletionsClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// StreamFn returns a StreamFn bound to this client.
func (c *OpenAICompletionsClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		body, err := buildOpenAIRequest(reqCtx, opts)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			strings.TrimRight(c.BaseURL, "/")+"/v1/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("content-type", "application/json")
		httpReq.Header.Set("authorization", "Bearer "+c.APIKey)
		httpReq.Header.Set("accept", "text/event-stream")

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
			return errorStreamProvider(opts.Model, "openai-completions",
				fmt.Sprintf("openai API error %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))), nil
		}

		s := NewEventStream(16)
		out := &AssistantMessage{Role: RoleAssistant, Content: []*Content{}, API: "openai-completions", Provider: "openai", Model: opts.Model, StopReason: StopPending}
		go func() {
			defer s.end()
			defer func() { _ = resp.Body.Close() }()
			streamOpenAISSE(ctx, resp.Body, out, s)
		}()
		return s, nil
	}
}

// StreamOpenAIReader runs the SSE→event core against a reader (offline tests).
func StreamOpenAIReader(ctx context.Context, r io.Reader, model string) *EventStream {
	s := NewEventStream(16)
	out := &AssistantMessage{Role: RoleAssistant, Content: []*Content{}, API: "openai-completions", Provider: "openai", Model: model, StopReason: StopPending}
	go func() {
		defer s.end()
		streamOpenAISSE(ctx, r, out, s)
	}()
	return s
}

func buildOpenAIRequest(reqCtx Context, opts Options) ([]byte, error) {
	msgs := make([]map[string]any, 0, len(reqCtx.Messages)+1)
	if reqCtx.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": reqCtx.System})
	}
	for _, m := range reqCtx.Messages {
		role := m.Role
		if role == "tool" || role == roleToolResult {
			role = "user" // simplified: tool results replayed as user text
		}
		msgs = append(msgs, map[string]any{"role": role, "content": m.Content})
	}

	req := map[string]any{
		"model":          opts.Model,
		"messages":       msgs,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if opts.MaxTokens > 0 {
		req["max_tokens"] = opts.MaxTokens
	}
	if len(reqCtx.Tools) > 0 {
		tools := make([]map[string]any, 0, len(reqCtx.Tools))
		for _, t := range reqCtx.Tools {
			tools = append(tools, map[string]any{
				"type":     "function",
				"function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters},
			})
		}
		req["tools"] = tools
	}
	return json.Marshal(req)
}

const roleToolResult = "toolResult"

type oaiChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func streamOpenAISSE(ctx context.Context, r io.Reader, out *AssistantMessage, s *EventStream) {
	if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
		return
	}

	textIdx := -1            // position of the text block in out.Content (-1 = none yet)
	toolPos := map[int]int{} // openai tool_call index -> position in out.Content

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
		return handleOpenAIChunk(ctx, payload, out, &textIdx, toolPos, s)
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

	// Finalize every open block in content order (pi's finishBlock loop).
	for i, c := range out.Content {
		switch c.Type {
		case KindText:
			if !s.push(ctx, Event{Type: EventTextEnd, ContentIndex: i, Content: c.Text, Partial: out}) {
				return
			}
		case KindToolCall:
			c.Arguments = parseStreamingJSON(c.partialJSON)
			c.partialJSON = ""
			if !s.push(ctx, Event{Type: EventToolCallEnd, ContentIndex: i, ToolCall: c, Partial: out}) {
				return
			}
		}
	}

	// If the provider never sent finish_reason, infer from content (compat).
	if out.StopReason == StopPending {
		if len(out.ToolCalls()) > 0 {
			out.StopReason = StopToolUse
		} else {
			out.StopReason = StopStop
		}
	}
	switch out.StopReason {
	case StopError, StopAborted:
		finishError(ctx, out, s, out.ErrorMessage)
	default:
		s.push(ctx, Event{Type: EventDone, Reason: out.StopReason, Message: out})
	}
}

func handleOpenAIChunk(ctx context.Context, payload string, out *AssistantMessage, textIdx *int, toolPos map[int]int, s *EventStream) bool {
	var chunk oaiChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return true
	}

	if chunk.Usage != nil {
		out.Usage.Input = chunk.Usage.PromptTokens
		out.Usage.Output = chunk.Usage.CompletionTokens
		out.Usage.TotalTokens = chunk.Usage.TotalTokens
		if out.Usage.TotalTokens == 0 {
			out.Usage.TotalTokens = out.Usage.Input + out.Usage.Output
		}
	}
	if len(chunk.Choices) == 0 {
		return true
	}
	choice := chunk.Choices[0]

	if choice.Delta.Content != "" {
		if *textIdx < 0 {
			out.Content = append(out.Content, &Content{Type: KindText})
			*textIdx = len(out.Content) - 1
			if !s.push(ctx, Event{Type: EventTextStart, ContentIndex: *textIdx, Partial: out}) {
				return false
			}
		}
		out.Content[*textIdx].Text += choice.Delta.Content
		if !s.push(ctx, Event{Type: EventTextDelta, ContentIndex: *textIdx, Delta: choice.Delta.Content, Partial: out}) {
			return false
		}
	}

	for _, tc := range choice.Delta.ToolCalls {
		pos, ok := toolPos[tc.Index]
		if !ok {
			out.Content = append(out.Content, &Content{Type: KindToolCall, ToolID: tc.ID, ToolName: tc.Function.Name})
			pos = len(out.Content) - 1
			toolPos[tc.Index] = pos
			if !s.push(ctx, Event{Type: EventToolCallStart, ContentIndex: pos, Partial: out}) {
				return false
			}
		}
		block := out.Content[pos]
		if block.ToolID == "" && tc.ID != "" {
			block.ToolID = tc.ID
		}
		if block.ToolName == "" && tc.Function.Name != "" {
			block.ToolName = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			block.partialJSON += tc.Function.Arguments
			block.Arguments = parseStreamingJSON(block.partialJSON)
			if !s.push(ctx, Event{Type: EventToolCallDelta, ContentIndex: pos, Delta: tc.Function.Arguments, Partial: out}) {
				return false
			}
		}
	}

	if choice.FinishReason != nil {
		out.RawStopReason = *choice.FinishReason
		out.StopReason, out.ErrorMessage = mapOpenAIFinishReason(*choice.FinishReason)
	}
	return true
}

func mapOpenAIFinishReason(reason string) (StopReason, string) {
	switch reason {
	case "stop", "end", "":
		return StopStop, ""
	case "length":
		return StopLength, ""
	case "tool_calls", "function_call":
		return StopToolUse, ""
	case "content_filter":
		return StopError, "Provider finish_reason: content_filter"
	default:
		return StopError, "Provider finish_reason: " + reason
	}
}

func errorStreamProvider(model, api, msg string) *EventStream {
	s := NewEventStream(1)
	out := &AssistantMessage{Role: RoleAssistant, Content: []*Content{}, API: api, Model: model, StopReason: StopError, ErrorMessage: msg}
	s.ch <- Event{Type: EventError, Reason: StopError, Message: out}
	close(s.ch)
	return s
}
