package ai

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

// openAIFixture is a recorded-style OpenAI Chat Completions SSE stream: text
// ("Hello, world") plus a streamed tool call to `read`, then finish_reason and usage.
const openAIFixture = `data: {"choices":[{"delta":{"role":"assistant","content":""}}]}

data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":", world"}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read","arguments":"{\"path\":\"REA"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"DME.md\"}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18}}

data: [DONE]
`

func TestStreamOpenAIReaderFixture(t *testing.T) {
	stream := StreamOpenAIReader(context.Background(), strings.NewReader(openAIFixture), "gpt-test")
	events, final := stream.Collect()

	hasEnd := map[EventType]bool{}
	for _, e := range events {
		hasEnd[e.Type] = true
	}
	if !hasEnd[EventTextEnd] || !hasEnd[EventToolCallEnd] {
		t.Errorf("missing text_end/toolcall_end; events=%v", eventTypes(events))
	}
	if events[len(events)-1].Type != EventDone {
		t.Fatalf("last event = %q, want done", events[len(events)-1].Type)
	}

	if final == nil {
		t.Fatal("no final message")
	}
	if final.Text() != "Hello, world" {
		t.Errorf("text = %q, want %q", final.Text(), "Hello, world")
	}
	if final.StopReason != StopToolUse {
		t.Errorf("stopReason = %q, want toolUse", final.StopReason)
	}
	calls := final.ToolCalls()
	if len(calls) != 1 || calls[0].ToolName != "read" || calls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool calls = %+v, want one read of README.md", calls)
	}
	if final.Usage.Input != 10 || final.Usage.Output != 8 || final.Usage.TotalTokens != 18 {
		t.Errorf("usage = %+v, want input=10 output=8 total=18", final.Usage)
	}
}

func TestStreamOpenAIReaderRejectsIncompleteToolArguments(t *testing.T) {
	fixture := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read","arguments":"{\"path\":\"foo\""}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
`
	_, final := StreamOpenAIReader(context.Background(), strings.NewReader(fixture), "gpt-test").Collect()
	if final == nil || len(final.ToolCalls()) != 1 || final.ToolCalls()[0].InvalidArguments == nil {
		t.Fatalf("expected invalid arguments, got %+v", final)
	}
	if got := *final.ToolCalls()[0].InvalidArguments; got != `{"path":"foo"` {
		t.Fatalf("raw arguments = %q", got)
	}
}

func TestStreamOpenAIReaderSplitsCacheUsage(t *testing.T) {
	cases := []struct {
		name  string
		usage string
		want  Usage
	}{
		{
			name:  "prompt details",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"prompt_tokens_details":{"cached_tokens":80}}`,
			want:  Usage{Input: 20, Output: 10, CacheRead: 80, TotalTokens: 110},
		},
		{
			name:  "kimi cached_tokens",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"cached_tokens":80}`,
			want:  Usage{Input: 20, Output: 10, CacheRead: 80, TotalTokens: 110},
		},
		{
			name:  "deepseek prompt_cache_hit_tokens",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"prompt_cache_hit_tokens":80}`,
			want:  Usage{Input: 20, Output: 10, CacheRead: 80, TotalTokens: 110},
		},
		{
			name:  "cache write stays separate",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":60,"cache_write_tokens":20}}`,
			want:  Usage{Input: 20, Output: 10, CacheRead: 60, CacheWrite: 20, TotalTokens: 110},
		},
		{
			name:  "details win over top-level",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"cached_tokens":5,"prompt_cache_hit_tokens":7,"prompt_tokens_details":{"cached_tokens":80}}`,
			want:  Usage{Input: 20, Output: 10, CacheRead: 80, TotalTokens: 110},
		},
		{
			name:  "zero details do not fall through",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"cached_tokens":80,"prompt_tokens_details":{"cached_tokens":0}}`,
			want:  Usage{Input: 100, Output: 10, TotalTokens: 110},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":" + tc.usage + "}\n\ndata: [DONE]\n"
			_, final := StreamOpenAIReader(context.Background(), strings.NewReader(body), "gpt-test").Collect()
			if final == nil {
				t.Fatal("no final message")
			}
			got := final.Usage
			if got.Input != tc.want.Input || got.Output != tc.want.Output || got.CacheRead != tc.want.CacheRead || got.CacheWrite != tc.want.CacheWrite || got.TotalTokens != tc.want.TotalTokens {
				t.Fatalf("usage = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestOpenAICompletionsJoinsBaseURL(t *testing.T) {
	cases := []struct {
		name     string
		suffix   string
		wantPath string
	}{
		{name: "host only", suffix: "", wantPath: "/v1/chat/completions"},
		{name: "trailing slash", suffix: "/", wantPath: "/v1/chat/completions"},
		{name: "already v1", suffix: "/v1", wantPath: "/v1/chat/completions"},
		{name: "v1 slash", suffix: "/v1/", wantPath: "/v1/chat/completions"},
		{name: "nested openai v1", suffix: "/openai/v1", wantPath: "/openai/v1/chat/completions"},
		{name: "v4 paas", suffix: "/api/coding/paas/v4", wantPath: "/api/coding/paas/v4/chat/completions"},
		{name: "cloudflare compat", suffix: "/compat", wantPath: "/compat/chat/completions"},
		{name: "cloudflare openai", suffix: "/openai", wantPath: "/openai/chat/completions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(openAIFixture))
			}))
			defer srv.Close()

			client := &OpenAICompletionsClient{BaseURL: srv.URL + tc.suffix, APIKey: "k", HTTPClient: srv.Client()}
			stream, err := client.StreamFn()(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "gpt-test"})
			if err != nil {
				t.Fatalf("StreamFn error: %v", err)
			}
			stream.Collect()
			if gotPath != tc.wantPath {
				t.Fatalf("path = %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}

const openAIReasoningFixture = `data: {"choices":[{"delta":{"reasoning_content":"plan "}}]}

data: {"choices":[{"delta":{"reasoning_content":"ahead"}}]}

data: {"choices":[{"delta":{"content":"done"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]
`

func TestStreamOpenAIReaderReasoningContent(t *testing.T) {
	stream := StreamOpenAIReader(context.Background(), strings.NewReader(openAIReasoningFixture), "gpt-test")
	events, final := stream.Collect()
	has := map[EventType]bool{}
	for _, e := range events {
		has[e.Type] = true
	}
	if !has[EventThinkingStart] || !has[EventThinkingEnd] || !has[EventTextEnd] {
		t.Fatalf("missing thinking/text events; events=%v", eventTypes(events))
	}
	if final == nil {
		t.Fatal("no final message")
	}
	if final.Text() != "done" {
		t.Errorf("text = %q, want done", final.Text())
	}
	var thinking string
	for _, c := range final.Content {
		if c.Type == KindThinking {
			thinking += c.Thinking
		}
	}
	if thinking != "plan ahead" {
		t.Errorf("thinking = %q, want %q", thinking, "plan ahead")
	}
}

const openAIReasoningAltFixture = `data: {"choices":[{"delta":{"reasoning":"alt"}}]}

data: {"choices":[{"delta":{"content":"ok"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]
`

func TestStreamOpenAIReaderReasoningFieldFallback(t *testing.T) {
	stream := StreamOpenAIReader(context.Background(), strings.NewReader(openAIReasoningAltFixture), "gpt-test")
	_, final := stream.Collect()
	if final == nil {
		t.Fatal("no final message")
	}
	var thinking string
	for _, c := range final.Content {
		if c.Type == KindThinking {
			thinking += c.Thinking
		}
	}
	if thinking != "alt" {
		t.Fatalf("thinking = %q, want alt", thinking)
	}
}

func TestOpenAICompletionsSessionAffinityHeaders(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "aff-comp", DefaultAPI: "openai-completions", DefaultID: "none",
		Models: []models.Model{
			{Provider: "aff-comp", ID: "openai", Compat: &models.Compat{SessionAffinityFormat: "openai"}},
			{Provider: "aff-comp", ID: "nosession", Compat: &models.Compat{SessionAffinityFormat: "openai-nosession"}},
			{Provider: "aff-comp", ID: "openrouter", Compat: &models.Compat{SessionAffinityFormat: "openrouter"}},
			{Provider: "aff-comp", ID: "none"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("aff-comp") })

	cases := []struct {
		model string
		want  map[string]string
		dont  []string
	}{
		{
			model: "openai",
			want: map[string]string{
				"session_id":          "sess-1",
				"x-client-request-id": "sess-1",
				"x-session-affinity":  "sess-1",
			},
		},
		{
			model: "nosession",
			want: map[string]string{
				"x-client-request-id": "sess-1",
				"x-session-affinity":  "sess-1",
			},
			dont: []string{"session_id", "x-session-id"},
		},
		{
			model: "openrouter",
			want:  map[string]string{"x-session-id": "sess-1"},
			dont:  []string{"session_id", "x-client-request-id", "x-session-affinity"},
		},
		{
			model: "none",
			dont:  []string{"session_id", "x-session-id", "x-session-affinity", "x-client-request-id"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			var got http.Header
			var payload map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &payload)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(openAIFixture))
			}))
			defer srv.Close()

			client := &OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
			stream, err := client.StreamFn()(context.Background(), Context{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, Options{Provider: "aff-comp", Model: tc.model, SessionID: "sess-1"})
			if err != nil {
				t.Fatal(err)
			}
			stream.Collect()
			for k, v := range tc.want {
				if got.Get(k) != v {
					t.Errorf("%s = %q, want %q", k, got.Get(k), v)
				}
			}
			for _, k := range tc.dont {
				if got.Get(k) != "" {
					t.Errorf("unexpected %s = %q", k, got.Get(k))
				}
			}
			if payload["prompt_cache_key"] != "sess-1" {
				t.Errorf("prompt_cache_key = %#v", payload["prompt_cache_key"])
			}
		})
	}
}

func TestOpenAICompletionsCatalogCost(t *testing.T) {
	const provider = "cost-comp"
	models.RegisterProvider(models.ProviderSpec{
		ID: provider, DefaultAPI: "openai-completions", DefaultID: "priced",
		Models: []models.Model{
			{Provider: provider, ID: "priced", API: "openai-completions", Cost: &models.Cost{Input: 3, Output: 15, CacheRead: 0.30, CacheWrite: 3.75}},
			{Provider: provider, ID: "free", API: "openai-completions"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider(provider) })

	run := func(model, usage string) Usage {
		t.Helper()
		body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":" + usage + "}\n\ndata: [DONE]\n"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, body)
		}))
		defer srv.Close()
		client := &OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
		stream, err := client.StreamFn()(context.Background(), Context{
			Messages: []Message{{Role: RoleUser, Content: "hi"}},
		}, Options{Provider: provider, Model: model})
		if err != nil {
			t.Fatal(err)
		}
		_, final := stream.Collect()
		if final == nil {
			t.Fatal("no final message")
		}
		return final.Usage
	}

	u := run("priced", `{"prompt_tokens":1000000,"completion_tokens":1000000,"total_tokens":2000000}`)
	if math.Abs(u.Cost.Input-3) > 1e-9 || math.Abs(u.Cost.Output-15) > 1e-9 || math.Abs(u.Cost.Total-18) > 1e-9 {
		t.Fatalf("priced cost = %+v, want total 18", u.Cost)
	}

	u = run("priced", `{"prompt_tokens":1000000,"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":500000,"cache_write_tokens":200000}}`)
	if u.Input != 300_000 || u.CacheRead != 500_000 || u.CacheWrite != 200_000 {
		t.Fatalf("cache tokens = %+v", u)
	}
	if math.Abs(u.Cost.Input-0.9) > 1e-9 || math.Abs(u.Cost.CacheRead-0.15) > 1e-9 || math.Abs(u.Cost.CacheWrite-0.75) > 1e-9 || math.Abs(u.Cost.Total-1.8) > 1e-9 {
		t.Fatalf("cache cost = %+v", u.Cost)
	}

	u = run("free", `{"prompt_tokens":1000000,"completion_tokens":1000000,"total_tokens":2000000}`)
	if u.Cost != (UsageCost{}) {
		t.Fatalf("free cost = %+v, want zeros", u.Cost)
	}
}

func TestStreamOpenAIMissingFinishReason(t *testing.T) {
	textOnly := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n"
	withTool := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]}}]}\n\ndata: [DONE]\n"
	withReason := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n"

	t.Run("default missing is an error", func(t *testing.T) {
		_, final := StreamOpenAIReader(context.Background(), strings.NewReader(textOnly), "gpt-test").Collect()
		if final == nil || final.StopReason != StopError || !strings.Contains(final.ErrorMessage, "Stream ended without finish_reason") {
			t.Fatalf("final = %+v", final)
		}
		if !IsRetryableAssistantError(final) {
			t.Fatalf("error should be retryable: %q", final.ErrorMessage)
		}
	})

	t.Run("default missing tool call is an error", func(t *testing.T) {
		events, final := StreamOpenAIReader(context.Background(), strings.NewReader(withTool), "gpt-test").Collect()
		if final == nil || final.StopReason != StopError {
			t.Fatalf("stop = %v, want error", final)
		}
		if !strings.Contains(final.ErrorMessage, "Stream ended without finish_reason") {
			t.Fatalf("error = %q", final.ErrorMessage)
		}
		if len(events) == 0 || events[len(events)-1].Type != EventError {
			t.Fatalf("last event = %v", events)
		}
	})

	t.Run("normal stream still stops", func(t *testing.T) {
		events, final := StreamOpenAIReader(context.Background(), strings.NewReader(withReason), "gpt-test").Collect()
		if final == nil || final.StopReason != StopStop || final.Text() != "hi" {
			t.Fatalf("final = %+v", final)
		}
		if events[len(events)-1].Type != EventDone {
			t.Fatalf("last event = %s", events[len(events)-1].Type)
		}
	})

	off := false
	on := true
	for _, tc := range []struct {
		name     string
		compat   *models.Compat
		body     string
		wantStop StopReason
	}{
		{name: "flag false infers stop", compat: &models.Compat{SupportsFinishReason: &off}, body: textOnly, wantStop: StopStop},
		{name: "flag false infers tool use", compat: &models.Compat{SupportsFinishReason: &off}, body: withTool, wantStop: StopToolUse},
		{name: "flag true stays strict", compat: &models.Compat{SupportsFinishReason: &on}, body: textOnly, wantStop: StopError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const provider = "finish-reason"
			models.RegisterProvider(models.ProviderSpec{
				ID: provider, DefaultAPI: "openai-completions", DefaultID: "m",
				Models: []models.Model{{Provider: provider, ID: "m", API: "openai-completions", Compat: tc.compat}},
			})
			t.Cleanup(func() { models.UnregisterProvider(provider) })
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			client := &OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
			stream, err := client.StreamFn()(context.Background(), Context{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, Options{Provider: provider, Model: "m"})
			if err != nil {
				t.Fatal(err)
			}
			_, final := stream.Collect()
			if final == nil || final.StopReason != tc.wantStop {
				t.Fatalf("stop = %v, want %s", final, tc.wantStop)
			}
			if tc.wantStop == StopError && !strings.Contains(final.ErrorMessage, "Stream ended without finish_reason") {
				t.Fatalf("error = %q", final.ErrorMessage)
			}
		})
	}
}

func eventTypes(events []Event) []EventType {
	out := make([]EventType, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}
