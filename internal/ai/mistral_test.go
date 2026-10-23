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

const mistralTextFixture = `data: {"id":"cmpl_1","choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":", world"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}

data: [DONE]
`

const mistralThinkingFixture = `data: {"choices":[{"delta":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"hmm"}]}]}}]}

data: {"choices":[{"delta":{"content":[{"type":"text","text":"ok"}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]
`

func TestMistralClientHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("authorization"), "Bearer ") {
			t.Errorf("auth = %q", r.Header.Get("authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(mistralTextFixture))
	}))
	defer srv.Close()

	client := &MistralClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "codestral-latest"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if final.API != "mistral-conversations" {
		t.Fatalf("api = %q", final.API)
	}
	if final.Usage.Input != 3 || final.Usage.Output != 2 {
		t.Fatalf("usage = %+v", final.Usage)
	}
}

func TestMistralSplitsCachedPromptTokens(t *testing.T) {
	cases := []struct {
		name  string
		usage string
		want  Usage
	}{
		{
			name:  "prompt_tokens_details",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"prompt_tokens_details":{"cached_tokens":80}}`,
			want:  Usage{Input: 20, Output: 10, CacheRead: 80, TotalTokens: 110},
		},
		{
			name:  "prompt_token_details",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"prompt_token_details":{"cached_tokens":40}}`,
			want:  Usage{Input: 60, Output: 10, CacheRead: 40, TotalTokens: 110},
		},
		{
			name:  "camel promptTokensDetails",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"promptTokensDetails":{"cachedTokens":80}}`,
			want:  Usage{Input: 20, Output: 10, CacheRead: 80, TotalTokens: 110},
		},
		{
			name:  "camel promptTokenDetails",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"promptTokenDetails":{"cachedTokens":25}}`,
			want:  Usage{Input: 75, Output: 10, CacheRead: 25, TotalTokens: 110},
		},
		{
			name:  "num_cached_tokens",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"num_cached_tokens":15}`,
			want:  Usage{Input: 85, Output: 10, CacheRead: 15, TotalTokens: 110},
		},
		{
			name:  "numCachedTokens",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"numCachedTokens":15}`,
			want:  Usage{Input: 85, Output: 10, CacheRead: 15, TotalTokens: 110},
		},
		{
			name:  "details win over later fields",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"num_cached_tokens":1,"promptTokensDetails":{"cachedTokens":2},"prompt_tokens_details":{"cached_tokens":80}}`,
			want:  Usage{Input: 20, Output: 10, CacheRead: 80, TotalTokens: 110},
		},
		{
			name:  "clamp to prompt",
			usage: `{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":150}}`,
			want:  Usage{Input: 0, Output: 10, CacheRead: 100, TotalTokens: 110},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":" + tc.usage + "}\n\ndata: [DONE]\n"
			s := NewEventStream(16)
			out := &AssistantMessage{Role: RoleAssistant, Content: []*Content{}, API: "mistral-conversations", StopReason: StopPending}
			go func() {
				defer s.end()
				streamMistralSSE(context.Background(), strings.NewReader(body), out, s, nil, nil)
			}()
			_, final := s.Collect()
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

func TestMistralThinkingContentArray(t *testing.T) {
	s := NewEventStream(16)
	out := &AssistantMessage{Role: RoleAssistant, Content: []*Content{}, API: "mistral-conversations", StopReason: StopPending}
	go func() {
		defer s.end()
		streamMistralSSE(context.Background(), strings.NewReader(mistralThinkingFixture), out, s, nil, nil)
	}()
	_, final := s.Collect()
	if final == nil || final.Text() != "ok" {
		t.Fatalf("text = %v", final)
	}
	if len(final.Content) < 1 || final.Content[0].Type != KindThinking || final.Content[0].Thinking != "hmm" {
		t.Fatalf("content = %+v", final.Content)
	}
}

func TestMistralToolCallIDLength(t *testing.T) {
	id := mistralToolCallID("toolu_long_identifier_12345")
	if len(id) != 9 {
		t.Fatalf("len = %d id=%q", len(id), id)
	}
	if mistralToolCallID("abc123xyz") != "abc123xyz" {
		t.Fatal("9-char id should be kept")
	}
}

func TestMistralWireMessagesNormalizesToolIDs(t *testing.T) {
	msgs := transformMessages([]Message{
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindThinking, Thinking: "plan"},
			{Type: KindText, Text: "hi"},
			{Type: KindToolCall, ToolID: "toolu_long_identifier_12345", ToolName: "read", Arguments: map[string]any{"p": "a"}},
		}}},
		{Role: RoleToolResult, ToolCallID: "toolu_long_identifier_12345", Content: "ok"},
	}, replayTarget{Provider: "mistral", API: "mistral-conversations", Model: "codestral"}, normalizeMistralToolCallID)
	got := mistralWireMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), `"type":"thinking"`) {
		t.Fatalf("cross-model thinking must be plain text: %s", raw)
	}
	if !strings.Contains(string(raw), "plan") {
		t.Fatalf("thinking text missing: %s", raw)
	}
	tcs, _ := got[0]["tool_calls"].([]map[string]any)
	id, _ := tcs[0]["id"].(string)
	if len(id) != 9 {
		t.Fatalf("tool id = %q", id)
	}
	if got[1]["tool_call_id"] != id {
		t.Fatalf("tool result id = %v want %s", got[1]["tool_call_id"], id)
	}
}

func TestStreamForMistral(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "codestral-latest") {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(mistralTextFixture))
	}))
	defer srv.Close()
	stream, err := StreamFor("mistral", ClientConfig{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})(
		context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "codestral-latest"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("%+v", final)
	}
}

func TestMistralCatalogCost(t *testing.T) {
	const provider = "cost-mist"
	models.RegisterProvider(models.ProviderSpec{
		ID: provider, DefaultAPI: "mistral-conversations", DefaultID: "priced",
		Models: []models.Model{
			{Provider: provider, ID: "priced", API: "mistral-conversations", Cost: &models.Cost{Input: 3, Output: 15, CacheRead: 0.30}},
			{Provider: provider, ID: "free", API: "mistral-conversations"},
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
		client := &MistralClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
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

	u = run("priced", `{"prompt_tokens":1000000,"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":400000}}`)
	if u.Input != 600_000 || u.CacheRead != 400_000 {
		t.Fatalf("cache tokens = %+v", u)
	}
	if math.Abs(u.Cost.Input-1.8) > 1e-9 || math.Abs(u.Cost.CacheRead-0.12) > 1e-9 || math.Abs(u.Cost.Total-1.92) > 1e-9 {
		t.Fatalf("cache cost = %+v", u.Cost)
	}

	u = run("free", `{"prompt_tokens":1000000,"completion_tokens":1000000,"total_tokens":2000000}`)
	if u.Cost != (UsageCost{}) {
		t.Fatalf("free cost = %+v, want zeros", u.Cost)
	}
}

func TestMistralFinishReasonErrorIsRetryable(t *testing.T) {
	const body = "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"error\"}]}\n\ndata: [DONE]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	client := &MistralClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "codestral-latest"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason != StopError || final.ErrorMessage != "Provider stopped with: error (server error)" {
		t.Fatalf("final=%+v", final)
	}
	if !IsRetryableAssistantError(final) {
		t.Fatal("finish_reason error should be retryable")
	}
	if IsContextOverflow(final, 0) {
		t.Fatal("finish_reason error is not overflow")
	}
}
