package ai

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genai"

	"github.com/Lowpower/pigo/internal/models"
)

func TestGoogleContentsRolesAndTools(t *testing.T) {
	got := googleContents(Context{Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindText, Text: "yo"},
			{Type: KindToolCall, ToolID: "1", ToolName: "read", Arguments: map[string]any{"path": "a"}},
		}}},
		{Role: RoleToolResult, ToolCallID: "1", ToolName: "read", Content: "ok"},
	}}, replayTarget{})
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Role != "user" || got[1].Role != "model" || got[2].Role != "user" {
		t.Fatalf("roles = %q %q %q", got[0].Role, got[1].Role, got[2].Role)
	}
	if got[1].Parts[1].FunctionCall == nil || got[1].Parts[1].FunctionCall.Name != "read" {
		t.Fatalf("tool part = %+v", got[1].Parts)
	}
	if got[2].Parts[0].FunctionResponse == nil {
		t.Fatal("missing function response")
	}
}

func TestGoogleVertexClientConfig(t *testing.T) {
	c := &GoogleClient{APIKey: "k", Project: "p", Location: "us-central1", Vertex: true}
	cfg := c.genaiConfig()
	if cfg.Backend != genai.BackendVertexAI || cfg.APIKey != "k" || cfg.Project != "p" || cfg.Location != "us-central1" {
		t.Fatalf("%+v", cfg)
	}
	if msg := c.missingAuth(); msg != "" {
		t.Fatalf("unexpected missingAuth %q", msg)
	}
}

func TestGoogleBaseURLHTTPOptions(t *testing.T) {
	c := &GoogleClient{APIKey: "k", BaseURL: "https://opencode.ai/zen"}
	cfg := c.genaiConfig()
	if cfg.HTTPOptions.BaseURL != "https://opencode.ai/zen" {
		t.Fatalf("HTTPOptions.BaseURL = %q", cfg.HTTPOptions.BaseURL)
	}
}

func TestApplyGoogleUsageSplitsCachedContent(t *testing.T) {
	var got Usage
	applyGoogleUsage(&got, &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:        100,
		CandidatesTokenCount:    10,
		CachedContentTokenCount: 80,
		ThoughtsTokenCount:      7,
		TotalTokenCount:         117,
	})
	if got.Input != 20 || got.Output != 10 || got.CacheRead != 80 || got.CacheWrite != 0 || got.TotalTokens != 110 {
		t.Fatalf("usage = %+v", got)
	}
}

func TestGoogleVertexMissingAuth(t *testing.T) {
	c := &GoogleClient{Vertex: true, Project: "p"}
	if msg := c.missingAuth(); msg == "" {
		t.Fatal("expected missing location")
	}
	stream, err := c.StreamFn()(context.Background(), Context{}, Options{Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason != StopError || final.API != "google-vertex" {
		t.Fatalf("%+v", final)
	}
}

func TestGoogleStreamPricesCatalogCost(t *testing.T) {
	const provider = "cost-goog"
	models.RegisterProvider(models.ProviderSpec{
		ID: provider, DefaultAPI: "google-generative-ai", DefaultID: "priced",
		Models: []models.Model{
			{Provider: provider, ID: "priced", API: "google-generative-ai", Cost: &models.Cost{Input: 3, Output: 15, CacheRead: 0.30}},
			{Provider: provider, ID: "free", API: "google-generative-ai"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider(provider) })

	run := func(model, usage string) Usage {
		t.Helper()
		body := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hi\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":" + usage + "}\n\n"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()
		client := &GoogleClient{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()}
		stream, err := client.StreamFn()(context.Background(), Context{
			Messages: []Message{{Role: RoleUser, Content: "hi"}},
		}, Options{Provider: provider, Model: model})
		if err != nil {
			t.Fatal(err)
		}
		_, final := stream.Collect()
		if final == nil || final.StopReason == StopError {
			t.Fatalf("final = %+v", final)
		}
		return final.Usage
	}

	u := run("priced", `{"promptTokenCount":1000000,"candidatesTokenCount":1000000,"totalTokenCount":2000000}`)
	if math.Abs(u.Cost.Input-3) > 1e-9 || math.Abs(u.Cost.Output-15) > 1e-9 || math.Abs(u.Cost.Total-18) > 1e-9 {
		t.Fatalf("priced cost = %+v, want total 18", u.Cost)
	}

	u = run("priced", `{"promptTokenCount":1000000,"candidatesTokenCount":0,"cachedContentTokenCount":400000}`)
	if u.Input != 600_000 || u.CacheRead != 400_000 {
		t.Fatalf("cache tokens = %+v", u)
	}
	if math.Abs(u.Cost.Input-1.8) > 1e-9 || math.Abs(u.Cost.CacheRead-0.12) > 1e-9 || math.Abs(u.Cost.Total-1.92) > 1e-9 {
		t.Fatalf("cache cost = %+v", u.Cost)
	}

	u = run("free", `{"promptTokenCount":1000000,"candidatesTokenCount":1000000,"totalTokenCount":2000000}`)
	if u.Cost != (UsageCost{}) {
		t.Fatalf("free cost = %+v, want zeros", u.Cost)
	}
}

func TestGoogleStreamFinishReasons(t *testing.T) {
	const toolPart = `{"functionCall":{"id":"call_1","name":"bash","args":{"command":"ls"}}}`
	cases := []struct {
		name      string
		candidate string
		wantStop  StopReason
		wantError string
		wantRaw   string
		wantEvent EventType
		wantTool  bool
		vertex    bool
	}{
		{
			name:      "stop",
			candidate: `{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}`,
			wantStop:  StopStop,
			wantRaw:   "STOP",
			wantEvent: EventDone,
		},
		{
			name:      "stop with tool",
			candidate: `{"content":{"role":"model","parts":[` + toolPart + `]},"finishReason":"STOP"}`,
			wantStop:  StopToolUse,
			wantRaw:   "STOP",
			wantEvent: EventDone,
			wantTool:  true,
		},
		{
			name:      "max tokens",
			candidate: `{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"MAX_TOKENS"}`,
			wantStop:  StopLength,
			wantRaw:   "MAX_TOKENS",
			wantEvent: EventDone,
		},
		{
			name:      "max tokens with tool",
			candidate: `{"content":{"role":"model","parts":[` + toolPart + `]},"finishReason":"MAX_TOKENS"}`,
			wantStop:  StopLength,
			wantRaw:   "MAX_TOKENS",
			wantEvent: EventDone,
			wantTool:  true,
		},
		{
			name:      "malformed function call",
			candidate: `{"content":{"role":"model","parts":[` + toolPart + `]},"finishReason":"MALFORMED_FUNCTION_CALL","finishMessage":"bad args"}`,
			wantStop:  StopError,
			wantError: "MALFORMED_FUNCTION_CALL: bad args",
			wantRaw:   "MALFORMED_FUNCTION_CALL",
			wantEvent: EventError,
			wantTool:  true,
			vertex:    true,
		},
		{
			// Gemini's response converter drops finishMessage. Vertex keeps it.
			name:      "malformed function call on gemini",
			candidate: `{"content":{"role":"model","parts":[` + toolPart + `]},"finishReason":"MALFORMED_FUNCTION_CALL","finishMessage":"bad args"}`,
			wantStop:  StopError,
			wantError: "MALFORMED_FUNCTION_CALL",
			wantRaw:   "MALFORMED_FUNCTION_CALL",
			wantEvent: EventError,
			wantTool:  true,
		},
		{
			name:      "unspecified",
			candidate: `{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"FINISH_REASON_UNSPECIFIED"}`,
			wantStop:  StopError,
			wantError: "FINISH_REASON_UNSPECIFIED",
			wantRaw:   "FINISH_REASON_UNSPECIFIED",
			wantEvent: EventError,
		},
		{
			name:      "empty finish reason",
			candidate: `{"content":{"role":"model","parts":[{"text":"hi"}]}}`,
			wantStop:  StopError,
			wantError: "FINISH_REASON_UNSPECIFIED",
			wantEvent: EventError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events, final := collectGoogleSSE(t, "data: {\"candidates\":["+tc.candidate+"]}\n\n", tc.vertex)
			if final == nil || final.StopReason != tc.wantStop {
				t.Fatalf("stop = %v, want %s", final, tc.wantStop)
			}
			if final.ErrorMessage != tc.wantError {
				t.Fatalf("error = %q, want %q", final.ErrorMessage, tc.wantError)
			}
			if final.RawStopReason != tc.wantRaw {
				t.Fatalf("raw = %q, want %q", final.RawStopReason, tc.wantRaw)
			}
			if events[len(events)-1].Type != tc.wantEvent {
				t.Fatalf("terminal = %s, want %s", events[len(events)-1].Type, tc.wantEvent)
			}
			calls := final.ToolCalls()
			if tc.wantTool {
				if len(calls) != 1 || calls[0].ToolName != "bash" || calls[0].ToolID != "call_1" {
					t.Fatalf("tools = %#v", calls)
				}
			} else if len(calls) != 0 {
				t.Fatalf("tools = %#v", calls)
			}
			if tc.wantStop == StopError && IsRetryableAssistantError(final) {
				t.Fatalf("finish reason error should not be retryable: %q", final.ErrorMessage)
			}
		})
	}
}

func TestGoogleStreamFinishReasonWithoutContent(t *testing.T) {
	body := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"partial\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"finishReason\":\"SAFETY\"}]}\n\n"
	_, final := collectGoogleSSE(t, body, false)
	if final == nil || final.StopReason != StopError || final.ErrorMessage != "SAFETY" || final.RawStopReason != "SAFETY" {
		t.Fatalf("%+v", final)
	}
	if final.Text() != "partial" {
		t.Fatalf("text = %q", final.Text())
	}
	if IsRetryableAssistantError(final) {
		t.Fatal("safety stop should not be retryable")
	}
}

func collectGoogleSSE(t *testing.T, body string, vertex bool) ([]Event, *AssistantMessage) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	client := &GoogleClient{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(), Vertex: vertex, Project: "p", Location: "us-central1"}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	return stream.Collect()
}
