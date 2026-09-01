package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/responses"
)

const defaultCodexBaseURL = "https://chatgpt.com/backend-api"

// OpenAICodexClient talks to ChatGPT's /codex/responses SSE endpoint.
type OpenAICodexClient struct {
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
}

func resolveCodexURL(base string) string {
	n := strings.TrimRight(strings.TrimSpace(base), "/")
	if n == "" {
		n = defaultCodexBaseURL
	}
	if strings.HasSuffix(n, "/codex/responses") {
		return n
	}
	if strings.HasSuffix(n, "/codex") {
		return n + "/responses"
	}
	return n + "/codex/responses"
}

func extractCodexAccountID(access string) string {
	parts := strings.Split(access, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		b, err2 := base64.StdEncoding.DecodeString(parts[1])
		if err2 != nil {
			return ""
		}
		payload = b
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
	id, _ := auth["chatgpt_account_id"].(string)
	return id
}

// StreamFn returns a StreamFn bound to this client.
func (c *OpenAICodexClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		if c.APIKey == "" {
			return errorStreamProvider(opts.Model, "openai-codex-responses", "no API key for provider: openai-codex"), nil
		}
		instructions := reqCtx.System
		if instructions == "" {
			instructions = "You are a helpful assistant."
		}
		bodyMap := map[string]any{
			"model":        opts.Model,
			"store":        false,
			"stream":       true,
			"instructions": instructions,
			"input":        buildResponsesInput(reqCtx),
		}
		if opts.MaxTokens > 0 {
			n := opts.MaxTokens
			if n < 16 {
				n = 16
			}
			bodyMap["max_output_tokens"] = n
		}
		if len(reqCtx.Tools) > 0 {
			tools := make([]map[string]any, 0, len(reqCtx.Tools))
			for _, t := range reqCtx.Tools {
				fn := map[string]any{
					"type":       "function",
					"name":       t.Name,
					"parameters": t.Parameters,
					"strict":     false,
				}
				if t.Description != "" {
					fn["description"] = t.Description
				}
				tools = append(tools, fn)
			}
			bodyMap["tools"] = tools
		}
		body, err := json.Marshal(bodyMap)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveCodexURL(c.BaseURL), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("content-type", "application/json")
		httpReq.Header.Set("accept", "text/event-stream")
		httpReq.Header.Set("authorization", "Bearer "+c.APIKey)
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
			return errorStreamProvider(opts.Model, "openai-codex-responses",
				fmt.Sprintf("Codex API error %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))), nil
		}
		s := NewEventStream(16)
		out := &AssistantMessage{
			Role: RoleAssistant, Content: []*Content{}, API: "openai-codex-responses",
			Provider: "openai-codex", Model: opts.Model, StopReason: StopPending,
		}
		go func() {
			defer s.end()
			defer func() { _ = resp.Body.Close() }()
			if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
				return
			}
			stream := ssestream.NewStream[responses.ResponseStreamEventUnion](ssestream.NewDecoder(resp), nil)
			if err := processResponsesStream(ctx, stream, out, s); err != nil && out.StopReason != StopError {
				finishError(ctx, out, s, err.Error())
				return
			}
			if out.StopReason == StopPending {
				if len(out.ToolCalls()) > 0 {
					out.StopReason = StopToolUse
				} else {
					out.StopReason = StopStop
				}
			}
			if out.StopReason == StopError || out.StopReason == StopAborted {
				finishError(ctx, out, s, out.ErrorMessage)
				return
			}
			s.push(ctx, Event{Type: EventDone, Reason: out.StopReason, Message: out})
		}()
		return s, nil
	}
}
