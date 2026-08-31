package ai

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

const mistralToolCallIDLength = 9

// MistralClient talks to Mistral's native /v1/chat/completions SSE endpoint.
type MistralClient struct {
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
}

// StreamFn returns a StreamFn bound to this client.
func (c *MistralClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		if c.APIKey == "" {
			return errorStreamProvider(opts.Model, "mistral-conversations", "no API key for provider: mistral"), nil
		}
		body, err := buildMistralRequest(reqCtx, opts)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL(c.BaseURL), bytes.NewReader(body))
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
			return errorStreamProvider(opts.Model, "mistral-conversations",
				fmt.Sprintf("Mistral API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(msg)))), nil
		}
		s := NewEventStream(16)
		out := &AssistantMessage{
			Role: RoleAssistant, Content: []*Content{}, API: "mistral-conversations",
			Provider: "mistral", Model: opts.Model, StopReason: StopPending,
		}
		go func() {
			defer s.end()
			defer func() { _ = resp.Body.Close() }()
			streamMistralSSE(ctx, resp.Body, out, s)
		}()
		return s, nil
	}
}

func buildMistralRequest(reqCtx Context, opts Options) ([]byte, error) {
	ids := map[string]string{}
	msgs := mistralWireMessages(reqCtx.Messages, ids)
	if reqCtx.System != "" {
		msgs = append([]map[string]any{{"role": "system", "content": reqCtx.System}}, msgs...)
	}
	req := map[string]any{
		"model":    opts.Model,
		"messages": msgs,
		"stream":   true,
	}
	if opts.MaxTokens > 0 {
		req["max_tokens"] = opts.MaxTokens
	}
	if len(reqCtx.Tools) > 0 {
		tools := make([]map[string]any, 0, len(reqCtx.Tools))
		for _, t := range reqCtx.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
					"strict":      false,
				},
			})
		}
		req["tools"] = tools
	}
	return json.Marshal(req)
}

func mistralWireMessages(msgs []Message, ids map[string]string) []map[string]any {
	norm := func(id string) string {
		if id == "" {
			return id
		}
		if v, ok := ids[id]; ok {
			return v
		}
		v := mistralToolCallID(id)
		ids[id] = v
		return v
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if m.Assistant != nil {
			var content []map[string]any
			var toolCalls []map[string]any
			for i, c := range m.Assistant.Content {
				switch c.Type {
				case KindText:
					if strings.TrimSpace(c.Text) != "" {
						content = append(content, map[string]any{"type": "text", "text": c.Text})
					}
				case KindThinking:
					if strings.TrimSpace(c.Thinking) != "" {
						content = append(content, map[string]any{
							"type":     "thinking",
							"thinking": []map[string]any{{"type": "text", "text": c.Thinking}},
						})
					}
				case KindToolCall:
					args, _ := json.Marshal(c.Arguments)
					if string(args) == "null" {
						args = []byte("{}")
					}
					toolCalls = append(toolCalls, map[string]any{
						"id":   norm(c.ToolID),
						"type": "function",
						"function": map[string]any{
							"name":      c.ToolName,
							"arguments": string(args),
						},
						"index": i,
					})
				}
			}
			msg := map[string]any{"role": "assistant"}
			if len(content) > 0 {
				msg["content"] = content
			}
			if len(toolCalls) > 0 {
				msg["tool_calls"] = toolCalls
			}
			out = append(out, msg)
			continue
		}
		if m.Role == RoleToolResult || m.ToolCallID != "" {
			text, _ := ParseToolContent(m.Content)
			if text == "" {
				text = m.Content
			}
			out = append(out, map[string]any{
				"role":         "tool",
				"tool_call_id": norm(m.ToolCallID),
				"content":      text,
			})
			continue
		}
		role := m.Role
		if role == "" {
			role = RoleUser
		}
		out = append(out, map[string]any{"role": role, "content": m.Content})
	}
	return out
}

func mistralToolCallID(id string) string {
	var cleaned strings.Builder
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cleaned.WriteRune(r)
		}
	}
	s := cleaned.String()
	if len(s) == mistralToolCallIDLength {
		return s
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:mistralToolCallIDLength]
}

type mistralChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Content   json.RawMessage `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
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

func streamMistralSSE(ctx context.Context, r io.Reader, out *AssistantMessage, s *EventStream) {
	if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
		return
	}
	textIdx, thinkIdx := -1, -1
	toolPos := map[int]int{}
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
		return handleMistralChunk(ctx, payload, out, &textIdx, &thinkIdx, toolPos, s)
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
	for i, c := range out.Content {
		switch c.Type {
		case KindText:
			s.push(ctx, Event{Type: EventTextEnd, ContentIndex: i, Content: c.Text, Partial: out})
		case KindThinking:
			s.push(ctx, Event{Type: EventThinkingEnd, ContentIndex: i, Content: c.Thinking, Partial: out})
		case KindToolCall:
			c.Arguments = parseStreamingJSON(c.partialJSON)
			c.partialJSON = ""
			s.push(ctx, Event{Type: EventToolCallEnd, ContentIndex: i, ToolCall: c, Partial: out})
		}
	}
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

func handleMistralChunk(ctx context.Context, payload string, out *AssistantMessage, textIdx, thinkIdx *int, toolPos map[int]int, s *EventStream) bool {
	var chunk mistralChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return true
	}
	if chunk.ID != "" && out.ResponseID == "" {
		out.ResponseID = chunk.ID
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
	texts, thinks := mistralContentDeltas(choice.Delta.Content)
	for _, delta := range thinks {
		if *thinkIdx < 0 {
			out.Content = append(out.Content, &Content{Type: KindThinking})
			*thinkIdx = len(out.Content) - 1
			if !s.push(ctx, Event{Type: EventThinkingStart, ContentIndex: *thinkIdx, Partial: out}) {
				return false
			}
		}
		out.Content[*thinkIdx].Thinking += delta
		if !s.push(ctx, Event{Type: EventThinkingDelta, ContentIndex: *thinkIdx, Delta: delta, Partial: out}) {
			return false
		}
	}
	for _, delta := range texts {
		if *textIdx < 0 {
			out.Content = append(out.Content, &Content{Type: KindText})
			*textIdx = len(out.Content) - 1
			if !s.push(ctx, Event{Type: EventTextStart, ContentIndex: *textIdx, Partial: out}) {
				return false
			}
		}
		out.Content[*textIdx].Text += delta
		if !s.push(ctx, Event{Type: EventTextDelta, ContentIndex: *textIdx, Delta: delta, Partial: out}) {
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
		if arg := mistralToolArgDelta(tc.Function.Arguments); arg != "" {
			block.partialJSON += arg
			block.Arguments = parseStreamingJSON(block.partialJSON)
			if !s.push(ctx, Event{Type: EventToolCallDelta, ContentIndex: pos, Delta: arg, Partial: out}) {
				return false
			}
		}
	}
	if choice.FinishReason != nil {
		out.RawStopReason = *choice.FinishReason
		out.StopReason, out.ErrorMessage = mapMistralFinishReason(*choice.FinishReason)
	}
	return true
}

func mistralContentDeltas(raw json.RawMessage) (texts, thinks []string) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			return []string{s}, nil
		}
		return nil, nil
	}
	var items []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking []struct {
			Text string `json:"text"`
		} `json:"thinking"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return nil, nil
	}
	for _, it := range items {
		switch it.Type {
		case "thinking":
			var b strings.Builder
			for _, p := range it.Thinking {
				b.WriteString(p.Text)
			}
			if b.Len() > 0 {
				thinks = append(thinks, b.String())
			}
		default:
			if it.Text != "" {
				texts = append(texts, it.Text)
			}
		}
	}
	return texts, thinks
}

func mistralToolArgDelta(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	return string(raw)
}

func mapMistralFinishReason(reason string) (StopReason, string) {
	switch reason {
	case "stop", "":
		return StopStop, ""
	case "length", "model_length":
		return StopLength, ""
	case "tool_calls":
		return StopToolUse, ""
	case "error":
		return StopError, "Provider stopped with: error"
	default:
		return StopError, "Provider stopped with: " + reason
	}
}
