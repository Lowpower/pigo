package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/responses"
)

const (
	defaultCodexBaseURL = "https://chatgpt.com/backend-api"
	openaiBetaSSE       = "responses=experimental"
	openaiBetaWS        = "responses_websockets=2026-02-06"
	zstdEncoderLevel    = zstd.SpeedDefault
)

// OpenAICodexClient talks to ChatGPT's /codex/responses SSE and WebSocket endpoints.
type OpenAICodexClient struct {
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
	// Transport is auto (default), sse, or websocket.
	Transport string
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

func resolveCodexWebSocketURL(base string) string {
	raw := resolveCodexURL(base)
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	return u.String()
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

func compressRequestBodyZstd(body []byte) []byte {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstdEncoderLevel))
	if err != nil {
		return nil
	}
	defer func() { _ = enc.Close() }()
	out := enc.EncodeAll(body, nil)
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *OpenAICodexClient) buildRequestBody(reqCtx Context, opts Options) (map[string]any, []byte, error) {
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
	return bodyMap, body, err
}

func (c *OpenAICodexClient) transport() string {
	switch strings.ToLower(strings.TrimSpace(c.Transport)) {
	case "sse", "websocket":
		return strings.ToLower(strings.TrimSpace(c.Transport))
	default:
		return "auto"
	}
}

func (c *OpenAICodexClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func skipCodexWebSocketHeader(k string) bool {
	switch strings.ToLower(k) {
	case "accept", "content-type", "openai-beta":
		return true
	default:
		return false
	}
}

func (c *OpenAICodexClient) websocketHeaders() http.Header {
	h := http.Header{}
	h.Set("authorization", "Bearer "+c.APIKey)
	for k, v := range c.Headers {
		if skipCodexWebSocketHeader(k) {
			continue
		}
		h.Set(k, v)
	}
	h.Set("OpenAI-Beta", openaiBetaWS)
	return h
}

func (c *OpenAICodexClient) dialWebSocket(ctx context.Context) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer
	if deadline, ok := ctx.Deadline(); ok {
		dialer.HandshakeTimeout = time.Until(deadline)
		if dialer.HandshakeTimeout <= 0 {
			return nil, ctx.Err()
		}
	}
	conn, resp, err := dialer.DialContext(ctx, resolveCodexWebSocketURL(c.BaseURL), c.websocketHeaders())
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return conn, err
}

func (c *OpenAICodexClient) wsEventStream(ctx context.Context, conn *websocket.Conn, bodyMap map[string]any, opts Options) *EventStream {
	s := NewEventStream(16)
	out := &AssistantMessage{
		Role: RoleAssistant, Content: []*Content{}, API: "openai-codex-responses",
		Provider: "openai-codex", Model: opts.Model, StopReason: StopPending,
	}
	go func() {
		defer s.end()
		defer func() { _ = conn.Close() }()
		payload := make(map[string]any, len(bodyMap)+1)
		for k, v := range bodyMap {
			payload[k] = v
		}
		payload["type"] = "response.create"
		if err := conn.WriteJSON(payload); err != nil {
			finishError(ctx, out, s, err.Error())
			return
		}
		if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
			return
		}
		pr, pw := io.Pipe()
		go func() {
			defer func() { _ = pw.Close() }()
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var meta struct {
					Type string `json:"type"`
				}
				_ = json.Unmarshal(data, &meta)
				switch meta.Type {
				case "response.done", "response.incomplete":
					var obj map[string]any
					if json.Unmarshal(data, &obj) == nil {
						obj["type"] = "response.completed"
						data, _ = json.Marshal(obj)
					}
				}
				if _, err := fmt.Fprintf(pw, "data: %s\n\n", data); err != nil {
					return
				}
				if meta.Type == "response.completed" || meta.Type == "response.done" || meta.Type == "response.incomplete" {
					return
				}
			}
		}()
		fake := &http.Response{StatusCode: http.StatusOK, Body: pr}
		stream := ssestream.NewStream[responses.ResponseStreamEventUnion](ssestream.NewDecoder(fake), nil)
		c.finishResponses(ctx, stream, out, s)
	}()
	return s
}

func (c *OpenAICodexClient) sseEventStream(ctx context.Context, body []byte, opts Options) (*EventStream, error) {
	send := body
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveCodexURL(c.BaseURL), bytes.NewReader(send))
	if err != nil {
		return nil, err
	}
	if compressed := compressRequestBodyZstd(body); compressed != nil {
		send = compressed
		httpReq.Body = io.NopCloser(bytes.NewReader(send))
		httpReq.ContentLength = int64(len(send))
		httpReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(send)), nil
		}
		httpReq.Header.Set("content-encoding", "zstd")
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	httpReq.Header.Set("authorization", "Bearer "+c.APIKey)
	for k, v := range c.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := c.httpClient().Do(httpReq)
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
		c.finishResponses(ctx, stream, out, s)
	}()
	return s, nil
}

func (c *OpenAICodexClient) finishResponses(ctx context.Context, stream *ssestream.Stream[responses.ResponseStreamEventUnion], out *AssistantMessage, s *EventStream) {
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
}

// StreamFn returns a StreamFn bound to this client.
func (c *OpenAICodexClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		if c.APIKey == "" {
			return errorStreamProvider(opts.Model, "openai-codex-responses", "no API key for provider: openai-codex"), nil
		}
		bodyMap, body, err := c.buildRequestBody(reqCtx, opts)
		if err != nil {
			return nil, err
		}
		tr := c.transport()
		if tr != "sse" {
			conn, err := c.dialWebSocket(ctx)
			if err == nil {
				return c.wsEventStream(ctx, conn, bodyMap, opts), nil
			}
			if tr == "websocket" {
				return errorStreamProvider(opts.Model, "openai-codex-responses",
					fmt.Sprintf("Codex websocket error: %s", err.Error())), nil
			}
		}
		return c.sseEventStream(ctx, body, opts)
	}
}
