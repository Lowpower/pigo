package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

const responsesFixture = `event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","delta":"Hello","content_index":0,"output_index":0,"sequence_number":1,"logprobs":[]}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","delta":", world","content_index":0,"output_index":0,"sequence_number":2,"logprobs":[]}

event: response.completed
data: {"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","status":"completed","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}

`

func TestOpenAIResponsesClientHTTP(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("authorization"), "Bearer ") {
			t.Errorf("missing Bearer authorization, got %q", r.Header.Get("authorization"))
		}
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			t.Errorf("path = %q, want .../responses", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("body json: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()

	client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-test", Thinking: "high", SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if final.API != "openai-responses" {
		t.Fatalf("api = %q", final.API)
	}
	if final.Usage.Input != 4 || final.Usage.Output != 2 {
		t.Fatalf("usage = %+v", final.Usage)
	}
	if payload["store"] != false {
		t.Errorf("store = %#v", payload["store"])
	}
	if payload["prompt_cache_key"] != "sess-1" {
		t.Errorf("prompt_cache_key = %#v", payload["prompt_cache_key"])
	}
	reasoning, _ := payload["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Errorf("reasoning = %#v", payload["reasoning"])
	}
	include, _ := payload["include"].([]any)
	if len(include) == 0 || include[0] != "reasoning.encrypted_content" {
		t.Errorf("include = %#v", payload["include"])
	}
}

func TestOpenAIResponsesPromptCacheOptions(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()

	client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Provider: "openai", Model: "gpt-6-astra", CacheRetention: "long", SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	opts, _ := payload["prompt_cache_options"].(map[string]any)
	if opts["ttl"] != "30m" {
		t.Fatalf("prompt_cache_options = %#v payload=%#v", opts, payload)
	}
	if _, ok := payload["prompt_cache_retention"]; ok {
		t.Fatalf("should not send 24h retention with explicit cache mode: %#v", payload)
	}
}

func TestStreamForAzureOpenAIResponses(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_VERSION", "")
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "gpt-4=my-dep")
	var gotModel, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotVersion = r.Header.Get("api-version")
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		gotModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()

	stream, err := StreamFor("azure-openai-responses", ClientConfig{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-4"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if final.API != "azure-openai-responses" {
		t.Fatalf("api = %q", final.API)
	}
	if gotVersion != "v1" {
		t.Fatalf("api-version = %q", gotVersion)
	}
	if gotModel != "my-dep" {
		t.Fatalf("model = %q, want my-dep", gotModel)
	}
}

func TestBuildResponsesInputToolPair(t *testing.T) {
	items := buildResponsesInput(Context{Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindToolCall, ToolID: "c1", ToolName: "read", Arguments: map[string]any{"path": "a"}},
		}}},
		{Role: RoleToolResult, ToolCallID: "c1", Content: "ok"},
	}}, replayTarget{})
	if len(items) < 3 {
		t.Fatalf("items = %d", len(items))
	}
}

func TestOpenAIResponsesSessionAffinityHeaders(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "aff-resp", DefaultAPI: "openai-responses", DefaultID: "none",
		Models: []models.Model{
			{Provider: "aff-resp", ID: "openai", Compat: &models.Compat{SessionAffinityFormat: "openai"}},
			{Provider: "aff-resp", ID: "nosession", Compat: &models.Compat{SessionAffinityFormat: "openai-nosession"}},
			{Provider: "aff-resp", ID: "openrouter", Compat: &models.Compat{SessionAffinityFormat: "openrouter"}},
			{Provider: "aff-resp", ID: "none"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("aff-resp") })

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
			},
			dont: []string{"x-session-id", "x-session-affinity"},
		},
		{
			model: "nosession",
			want:  map[string]string{"x-client-request-id": "sess-1"},
			dont:  []string{"session_id", "x-session-id", "x-session-affinity"},
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
				_, _ = w.Write([]byte(responsesFixture))
			}))
			defer srv.Close()

			client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
			stream, err := client.StreamFn()(context.Background(), Context{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, Options{Provider: "aff-resp", Model: tc.model, SessionID: "sess-1"})
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

func TestOpenAIResponsesMaxOutputTokensCompat(t *testing.T) {
	omit, send := false, true
	models.RegisterProvider(models.ProviderSpec{
		ID: "maxout-resp", DefaultAPI: "openai-responses", DefaultID: "unset",
		Models: []models.Model{
			{Provider: "maxout-resp", ID: "omit", Compat: &models.Compat{SupportsMaxOutputTokens: &omit}},
			{Provider: "maxout-resp", ID: "send", Compat: &models.Compat{SupportsMaxOutputTokens: &send}},
			{Provider: "maxout-resp", ID: "unset"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("maxout-resp") })

	cases := []struct {
		name, model string
		maxTokens   int
		wantPresent bool
		want        int
	}{
		{name: "false omits", model: "omit", maxTokens: 100},
		{name: "true sends", model: "send", maxTokens: 100, wantPresent: true, want: 100},
		{name: "true clamps below 16", model: "send", maxTokens: 10, wantPresent: true, want: 16},
		{name: "unset sends", model: "unset", maxTokens: 100, wantPresent: true, want: 100},
		{name: "false without MaxTokens omits", model: "omit"},
		{name: "unset without MaxTokens omits", model: "unset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &payload)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(responsesFixture))
			}))
			defer srv.Close()

			client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
			stream, err := client.StreamFn()(context.Background(), Context{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, Options{Provider: "maxout-resp", Model: tc.model, MaxTokens: tc.maxTokens})
			if err != nil {
				t.Fatal(err)
			}
			stream.Collect()

			got, ok := payload["max_output_tokens"]
			if !tc.wantPresent {
				if ok {
					t.Fatalf("unexpected max_output_tokens = %#v", got)
				}
				return
			}
			if !ok {
				t.Fatalf("missing max_output_tokens in %#v", payload)
			}
			n, _ := got.(float64)
			if int(n) != tc.want {
				t.Fatalf("max_output_tokens = %#v, want %d", got, tc.want)
			}
		})
	}
}

func responsesTierFixture(tier string, include bool) string {
	field := ""
	if include {
		field = fmt.Sprintf(`,"service_tier":%q`, tier)
	}
	return fmt.Sprintf(`event: response.completed
data: {"type":"response.completed","sequence_number":1,"response":{"id":"resp_tier","status":"completed"%s,"usage":{"input_tokens":1000000,"output_tokens":500000,"total_tokens":1500000}}}

`, field)
}

func TestOpenAIResponsesServiceTierPricing(t *testing.T) {
	const provider = "tier-price"
	models.RegisterProvider(models.ProviderSpec{
		ID: provider, DefaultAPI: "openai-responses", DefaultID: "priced",
		Models: []models.Model{{
			Provider: provider, ID: "priced", API: "openai-responses",
			Cost: &models.Cost{Input: 2, Output: 4, CacheRead: 0.5, CacheWrite: 2},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider(provider) })

	// 1_000_000 input @ $2/M and 500_000 output @ $4/M.
	standard := UsageCost{Input: 2, Output: 2, Total: 4}
	priority := UsageCost{Input: 4, Output: 4, Total: 8}
	cases := []struct {
		name    string
		tier    string
		include bool
		want    UsageCost
	}{
		{name: "fast", tier: "fast", include: true, want: priority},
		{name: "priority", tier: "priority", include: true, want: priority},
		{name: "default", tier: "default", include: true, want: standard},
		{name: "missing", want: standard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &payload)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, responsesTierFixture(tc.tier, tc.include))
			}))
			defer srv.Close()

			client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
			stream, err := client.StreamFn()(context.Background(), Context{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, Options{Provider: provider, Model: "priced"})
			if err != nil {
				t.Fatal(err)
			}
			_, final := stream.Collect()
			if final == nil {
				t.Fatal("missing final message")
			}
			if _, ok := payload["service_tier"]; ok {
				t.Fatalf("request sent service_tier: %#v", payload["service_tier"])
			}
			got := final.Usage.Cost
			if math.Abs(got.Input-tc.want.Input) > 1e-9 || math.Abs(got.Output-tc.want.Output) > 1e-9 || math.Abs(got.Total-tc.want.Total) > 1e-9 {
				t.Fatalf("cost = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func sseData(events ...string) string {
	var b strings.Builder
	for _, ev := range events {
		b.WriteString("data: ")
		b.WriteString(ev)
		b.WriteString("\n\n")
	}
	return b.String()
}

func collectResponsesSSE(t *testing.T, body string) ([]Event, *AssistantMessage) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	return stream.Collect()
}

func TestResponsesStreamParallelToolCallsWithOutputIndex(t *testing.T) {
	events, final := collectResponsesSSE(t, sseData(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"bash","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_a","delta":"{\"command\":\"echo a\"}"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"bash","arguments":"{\"command\":\"echo a\"}"}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_b","call_id":"call_b","name":"read","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"item_id":"fc_b","delta":"{\"path\":\"b\"}"}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_b","call_id":"call_b","name":"read","arguments":"{\"path\":\"b\"}"}}`,
		`{"type":"response.completed","response":{"id":"resp_tools","status":"completed","usage":{"input_tokens":2,"output_tokens":2,"total_tokens":4}}}`,
	))
	if final == nil || final.StopReason != StopToolUse {
		t.Fatalf("stop = %v", final)
	}
	calls := final.ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("calls = %#v", calls)
	}
	if calls[0].ToolName != "bash" || calls[0].ToolID != "call_a" || calls[0].Arguments["command"] != "echo a" {
		t.Fatalf("call0 = %#v", calls[0])
	}
	if calls[1].ToolName != "read" || calls[1].ToolID != "call_b" || calls[1].Arguments["path"] != "b" {
		t.Fatalf("call1 = %#v", calls[1])
	}
	var ends int
	for _, ev := range events {
		if ev.Type == EventToolCallEnd {
			ends++
		}
	}
	if ends != 2 {
		t.Fatalf("toolcall_end = %d", ends)
	}
}

func TestResponsesStreamParallelToolCallsWithoutOutputIndex(t *testing.T) {
	events, final := collectResponsesSSE(t, sseData(
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"bash","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_a","delta":"{\"command\":\"echo a\"}"}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_b","call_id":"call_b","name":"bash","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_b","delta":"{\"command\":\"echo b\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"bash","arguments":"{\"command\":\"echo a\"}"}}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_b","call_id":"call_b","name":"bash","arguments":"{\"command\":\"echo b\"}"}}`,
		`{"type":"response.completed","response":{"id":"resp_no_output_index","status":"completed"}}`,
	))
	if final == nil || final.StopReason != StopError {
		t.Fatalf("stop = %v", final)
	}
	if !strings.Contains(final.ErrorMessage, "unfinished tool call") || !strings.Contains(final.ErrorMessage, "bash") || !strings.Contains(final.ErrorMessage, "call_a") {
		t.Fatalf("error = %q", final.ErrorMessage)
	}
	if IsRetryableAssistantError(final) {
		t.Fatalf("unfinished tool call should not be retryable: %q", final.ErrorMessage)
	}
	for _, ev := range events {
		if ev.Type == EventToolCallEnd {
			t.Fatal("unfinished tool call emitted toolcall_end")
		}
	}
}

func TestResponsesStreamUnfinishedToolCall(t *testing.T) {
	events, final := collectResponsesSSE(t, sseData(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"bash","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"command\":\"rm"}`,
		`{"type":"response.completed","response":{"id":"resp_unfinished","status":"completed"}}`,
	))
	if final == nil || final.StopReason != StopError {
		t.Fatalf("stop = %v", final)
	}
	if !strings.Contains(final.ErrorMessage, "unfinished tool call") || !strings.Contains(final.ErrorMessage, "call_1") {
		t.Fatalf("error = %q", final.ErrorMessage)
	}
	for _, ev := range events {
		if ev.Type == EventToolCallEnd {
			t.Fatal("unfinished tool call emitted toolcall_end")
		}
	}
}

func TestResponsesStreamEndsBeforeTerminalEvent(t *testing.T) {
	_, final := collectResponsesSSE(t, sseData(
		`{"type":"response.output_text.delta","item_id":"msg_1","delta":"partial","output_index":0}`,
	))
	if final == nil || final.StopReason != StopError {
		t.Fatalf("stop = %v", final)
	}
	if !strings.Contains(final.ErrorMessage, "stream ended before a terminal response event") {
		t.Fatalf("error = %q", final.ErrorMessage)
	}
	if !IsRetryableAssistantError(final) {
		t.Fatalf("want retryable: %q", final.ErrorMessage)
	}
}

func TestResponsesStreamIncompleteReasons(t *testing.T) {
	cases := []struct {
		name      string
		reason    string
		wantStop  StopReason
		wantError string
		retryable bool
	}{
		{name: "max output tokens", reason: "max_output_tokens", wantStop: StopLength},
		{name: "content filter", reason: "content_filter", wantStop: StopError, wantError: "Response incomplete: content_filter"},
		{name: "unknown reason", reason: "max_time_limit", wantStop: StopError, wantError: "Response incomplete: max_time_limit"},
		{name: "missing reason", wantStop: StopError, wantError: "Response incomplete without a provider reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			details := ""
			if tc.reason != "" {
				details = fmt.Sprintf(`,"incomplete_details":{"reason":%q}`, tc.reason)
			}
			body := sseData(fmt.Sprintf(
				`{"type":"response.incomplete","response":{"id":"resp_inc","status":"incomplete"%s,"usage":{"input_tokens":3,"output_tokens":9,"total_tokens":12}}}`,
				details,
			))
			_, final := collectResponsesSSE(t, body)
			if final == nil || final.StopReason != tc.wantStop {
				t.Fatalf("stop = %v, want %s", final, tc.wantStop)
			}
			if tc.wantStop == StopLength {
				if final.Usage.Input != 3 || final.Usage.Output != 9 {
					t.Fatalf("usage = %+v", final.Usage)
				}
				return
			}
			if final.ErrorMessage != tc.wantError {
				t.Fatalf("error = %q, want %q", final.ErrorMessage, tc.wantError)
			}
			if IsRetryableAssistantError(final) {
				t.Fatalf("incomplete error should not be retryable: %q", final.ErrorMessage)
			}
		})
	}
}

func TestResponsesStreamFailedEvent(t *testing.T) {
	_, final := collectResponsesSSE(t, sseData(
		`{"type":"response.failed","response":{"id":"resp_failed","status":"failed","error":{"code":"server_error","message":"boom"}}}`,
	))
	if final == nil || final.StopReason != StopError {
		t.Fatalf("stop = %v", final)
	}
	if !strings.Contains(final.ErrorMessage, "server_error") || !strings.Contains(final.ErrorMessage, "boom") {
		t.Fatalf("error = %q", final.ErrorMessage)
	}
	if strings.Contains(final.ErrorMessage, "stream ended before a terminal response event") {
		t.Fatalf("failed event treated as a missing terminal: %q", final.ErrorMessage)
	}
}

func TestResponsesStreamTerminalEventWithoutTrailingBlank(t *testing.T) {
	body := strings.TrimSuffix(sseData(
		`{"type":"response.output_text.delta","item_id":"msg_1","delta":"Hi","output_index":0}`,
		`{"type":"response.completed","response":{"id":"resp_eof","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	), "\n")
	_, final := collectResponsesSSE(t, body)
	if final == nil || final.StopReason != StopStop {
		t.Fatalf("stop = %v", final)
	}
	if final.Text() != "Hi" {
		t.Fatalf("text = %q", final.Text())
	}
	if final.Usage.Input != 1 || final.Usage.Output != 1 {
		t.Fatalf("usage = %+v", final.Usage)
	}
}
