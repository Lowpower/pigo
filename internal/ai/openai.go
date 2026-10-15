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

	"github.com/Lowpower/pigo/internal/models"
)

// This file streams OpenAI Chat Completions SSE: text, reasoning_content
// thinking, tool-call streaming, finish_reason mapping, and usage.

// OpenAICompletionsClient talks to an OpenAI-compatible /v1/chat/completions
// endpoint. BaseURL and APIKey are configurable; auth is Bearer.
type OpenAICompletionsClient struct {
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
}

// StreamFn returns a StreamFn bound to this client.
func (c *OpenAICompletionsClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		body, err := buildOpenAIRequestAt(reqCtx, opts, c.BaseURL)
		if err != nil {
			return nil, err
		}
		body = transformRequestBody(opts, body)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			chatCompletionsURL(c.BaseURL), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("content-type", "application/json")
		if c.APIKey != "" {
			httpReq.Header.Set("authorization", "Bearer "+c.APIKey)
		}
		httpReq.Header.Set("accept", "text/event-stream")
		for k, v := range c.Headers {
			httpReq.Header.Set(k, v)
		}
		for k, v := range sessionAffinityHeaders(opts, affinityCompletions) {
			httpReq.Header.Set(k, v)
		}
		applyBasetenAffinityHeaders(httpReq.Header, c.Headers, opts)
		applyOpenCodeSessionHeader(httpReq.Header, c.Headers, opts)

		client := c.HTTPClient
		if client == nil {
			client = defaultHTTPClient()
		}
		resp, err := doHTTP(client, httpReq, opts)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return errorStreamOpenAI(chatProvider(opts), opts.Model, openAIHTTPError(resp.StatusCode, string(msg), isCerebras(opts))), nil
		}

		s := NewEventStream(16)
		out := &AssistantMessage{Role: RoleAssistant, Content: []*Content{}, API: "openai-completions", Provider: chatProvider(opts), Model: opts.Model, StopReason: StopPending}
		go func() {
			defer s.end()
			defer func() { _ = resp.Body.Close() }()
			streamOpenAISSE(ctx, resp.Body, out, s, opts.OnProviderStreamEvent, catalogCost(opts))
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
		streamOpenAISSE(ctx, r, out, s, nil, nil)
	}()
	return s
}

// chatCompletionsURL joins a provider base URL with /chat/completions.
// Bases that already end in /v1 or /v4 (OpenAI SDK style, groq, zai) must not
// get another /v1 segment. Cloudflare AI Gateway /compat and /openai are also
// SDK-style bases (POST .../compat/chat/completions, not .../compat/v1/...).
func chatCompletionsURL(base string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") || strings.HasSuffix(base, "/v4") ||
		strings.HasSuffix(base, "/compat") || strings.HasSuffix(base, "/openai") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

func buildOpenAIRequest(reqCtx Context, opts Options) ([]byte, error) {
	return buildOpenAIRequestAt(reqCtx, opts, "")
}

func buildOpenAIRequestAt(reqCtx Context, opts Options, baseURL string) ([]byte, error) {
	compat := resolveOpenAICompat(opts, baseURL)
	msgs := make([]map[string]any, 0, len(reqCtx.Messages)+1)
	if reqCtx.System != "" {
		role := "system"
		if compat.developerRole() {
			role = "developer"
		}
		msgs = append(msgs, map[string]any{"role": role, "content": reqCtx.System})
	}
	replayed := transformMessages(reqCtx.Messages, replayTarget{
		Provider: chatProvider(opts),
		API:      "openai-completions",
		Model:    opts.Model,
	}, normalizeOpenAIChatToolCallID(chatProvider(opts)))
	msgs = append(msgs, openAIWireMessages(replayed, openAIWireOpts{
		toolResultName:     compat.requiresToolResultName,
		assistantAfterTool: compat.requiresAssistantAfterToolResult,
		thinkingAsText:     compat.requiresThinkingAsText,
		emptyReasoning:     compat.emptyReasoningContent(),
	})...)

	req := map[string]any{
		"model":    opts.Model,
		"messages": msgs,
		"stream":   true,
	}
	if compat.supportsUsageInStreaming {
		req["stream_options"] = map[string]any{"include_usage": true}
	}
	limited := opts.MaxTokens > 0
	if limited {
		req[compat.maxTokensField] = opts.MaxTokens
	}
	if effort := reasoningEffort(opts); effort != "" {
		req["reasoning_effort"] = effort
	}
	if key := clampPromptCacheKey(opts.SessionID); key != "" {
		req["prompt_cache_key"] = key
	}
	if compat.supportsStore {
		req["store"] = false
	}
	var tools []map[string]any
	if len(reqCtx.Tools) > 0 {
		tools = make([]map[string]any, 0, len(reqCtx.Tools))
		for _, t := range reqCtx.Tools {
			parameters := t.Parameters
			strict := sendChatStrict(t, opts)
			if strict {
				parameters = strictToolParameters(t.Parameters)
			}
			fn := map[string]any{"name": t.Name, "description": t.Description, "parameters": parameters}
			if strict {
				fn["strict"] = true
			}
			tools = append(tools, map[string]any{
				"type":     "function",
				"function": fn,
			})
		}
		req["tools"] = tools
		if compat.zaiToolStream {
			req["tool_stream"] = true
		}
	}
	if compat.cacheControlFormat == "anthropic" && !strings.EqualFold(strings.TrimSpace(opts.CacheRetention), "none") {
		applyOpenAICacheControl(msgs, tools, opts, compat)
	}
	applyThinkingFormat(req, opts)
	if field := compat.thinkingBudgetField; field != "" {
		if budget := clampedThinkingBudget(opts, compat.model, opts.MaxTokens); budget > 0 {
			req[field] = budget
		}
	}
	if compat.openRouterRouting != nil {
		req["provider"] = compat.openRouterRouting
	}
	if gateway := vercelGatewayOptions(compat.vercel); gateway != nil {
		req["providerOptions"] = map[string]any{"gateway": gateway}
	}
	applySamplingParams(req, ResolveSamplingParams(opts.Provider, opts.Model, opts.SamplingParams))
	if limited {
		if compat.maxTokensField != "max_tokens" {
			delete(req, "max_tokens")
		}
		if compat.maxTokensField != "max_completion_tokens" {
			delete(req, "max_completion_tokens")
		}
	}
	return json.Marshal(req)
}

type oaiChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ReasoningText    string `json:"reasoning_text"`
			ToolCalls        []struct {
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
	Usage *oaiUsage `json:"usage"`
}

// oaiUsage is a Chat Completions usage object. PromptTokens includes cache
// hits. Providers disagree on where the hit count lives.
type oaiUsage struct {
	PromptTokens         int               `json:"prompt_tokens"`
	CompletionTokens     int               `json:"completion_tokens"`
	CachedTokens         *int              `json:"cached_tokens"`
	PromptCacheHitTokens *int              `json:"prompt_cache_hit_tokens"`
	PromptTokensDetails  *oaiPromptDetails `json:"prompt_tokens_details"`
}

type oaiPromptDetails struct {
	CachedTokens     *int `json:"cached_tokens"`
	CacheWriteTokens *int `json:"cache_write_tokens"`
}

// openAICacheCounts reads cache hits from prompt_tokens_details, then
// prompt_cache_hit_tokens, then top-level cached_tokens. A present field wins
// even when it is zero, so a later placement is not added on top. Cache writes
// are a separate count and are not folded into the hit count.
func openAICacheCounts(u oaiUsage) (read, write int) {
	switch {
	case u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens != nil:
		read = *u.PromptTokensDetails.CachedTokens
	case u.PromptCacheHitTokens != nil:
		read = *u.PromptCacheHitTokens
	case u.CachedTokens != nil:
		read = *u.CachedTokens
	}
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CacheWriteTokens != nil {
		write = *u.PromptTokensDetails.CacheWriteTokens
	}
	return read, write
}

func streamOpenAISSE(ctx context.Context, r io.Reader, out *AssistantMessage, s *EventStream, observe func([]byte), cost *models.Cost) {
	if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
		return
	}

	textIdx := -1            // position of the text block in out.Content (-1 = none yet)
	thinkIdx := -1           // position of the thinking block in out.Content (-1 = none yet)
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
		return handleOpenAIChunk(ctx, payload, out, &textIdx, &thinkIdx, toolPos, s, observe, cost)
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

	// Finalize every open block in content order.
	for i, c := range out.Content {
		switch c.Type {
		case KindText:
			if !s.push(ctx, Event{Type: EventTextEnd, ContentIndex: i, Content: c.Text, Partial: out}) {
				return
			}
		case KindThinking:
			if !s.push(ctx, Event{Type: EventThinkingEnd, ContentIndex: i, Content: c.Thinking, Partial: out}) {
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

func handleOpenAIChunk(ctx context.Context, payload string, out *AssistantMessage, textIdx, thinkIdx *int, toolPos map[int]int, s *EventStream, observe func([]byte), cost *models.Cost) bool {
	var chunk oaiChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return true
	}
	emitProviderStreamEvent(observe, []byte(payload))

	if chunk.Usage != nil {
		read, write := openAICacheCounts(*chunk.Usage)
		applyInclusivePromptUsage(&out.Usage, chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, read, write)
		calculateCost(cost, &out.Usage)
	}
	if len(chunk.Choices) == 0 {
		return true
	}
	choice := chunk.Choices[0]

	if delta := firstReasoningDelta(choice.Delta.ReasoningContent, choice.Delta.Reasoning, choice.Delta.ReasoningText); delta != "" {
		if *thinkIdx < 0 {
			out.Content = append(out.Content, &Content{Type: KindThinking, ThinkingSignature: "reasoning_content"})
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

func firstReasoningDelta(fields ...string) string {
	for _, s := range fields {
		if s != "" {
			return s
		}
	}
	return ""
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

func chatProvider(opts Options) string {
	return recordedProvider(opts, "openai")
}

func openAIHTTPError(status int, body string, cerebras bool) string {
	body = strings.TrimSpace(body)
	if cerebras && body == "" && (status == 400 || status == 413) {
		return fmt.Sprintf("%d status code (no body)", status)
	}
	return fmt.Sprintf("openai API error %d: %s", status, body)
}

func errorStreamOpenAI(provider, model, msg string) *EventStream {
	s := NewEventStream(1)
	out := &AssistantMessage{Role: RoleAssistant, Content: []*Content{}, API: "openai-completions", Provider: provider, Model: model, StopReason: StopError, ErrorMessage: msg}
	s.ch <- Event{Type: EventError, Reason: StopError, Message: out}
	close(s.ch)
	return s
}

func errorStreamProvider(model, api, msg string) *EventStream {
	s := NewEventStream(1)
	out := &AssistantMessage{Role: RoleAssistant, Content: []*Content{}, API: api, Model: model, StopReason: StopError, ErrorMessage: msg}
	s.ch <- Event{Type: EventError, Reason: StopError, Message: out}
	close(s.ch)
	return s
}
