package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

// anthropicFixture is a recorded-style Anthropic Messages SSE stream: a text
// block ("Hello, world") followed by a tool_use call to `read` whose arguments
// arrive as two input_json_delta chunks, then a tool_use stop.
const anthropicFixture = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu_1","name":"read","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"READ"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"ME.md\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":25}}

event: message_stop
data: {"type":"message_stop"}
`

func TestStreamAnthropicReaderFixture(t *testing.T) {
	stream := StreamAnthropicReader(context.Background(), strings.NewReader(anthropicFixture), "claude-test", nil)
	events, final := stream.Collect()

	var types []EventType
	for _, e := range events {
		types = append(types, e.Type)
	}
	want := []EventType{
		EventStart,
		EventTextStart, EventTextDelta, EventTextDelta, EventTextEnd,
		EventToolCallStart, EventToolCallDelta, EventToolCallDelta, EventToolCallEnd,
		EventDone,
	}
	if fmt.Sprint(types) != fmt.Sprint(want) {
		t.Fatalf("event sequence =\n  %v\nwant\n  %v", types, want)
	}

	if final == nil {
		t.Fatal("no final message")
	}
	if got := final.Text(); got != "Hello, world" {
		t.Errorf("text = %q, want %q", got, "Hello, world")
	}
	if final.StopReason != StopToolUse {
		t.Errorf("stopReason = %q, want %q", final.StopReason, StopToolUse)
	}
	calls := final.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
	if calls[0].ToolName != "read" {
		t.Errorf("tool name = %q, want read", calls[0].ToolName)
	}
	if calls[0].Arguments["path"] != "README.md" {
		t.Errorf("tool args path = %v, want README.md", calls[0].Arguments["path"])
	}
	if final.Usage.Input != 10 || final.Usage.Output != 25 || final.Usage.TotalTokens != 35 {
		t.Errorf("usage = %+v, want input=10 output=25 total=35", final.Usage)
	}

	// The toolcall_end event carries the finalized tool call with parsed args.
	for _, e := range events {
		if e.Type == EventToolCallEnd {
			if e.ToolCall == nil || e.ToolCall.Arguments["path"] != "README.md" {
				t.Errorf("toolcall_end args = %v, want path=README.md", e.ToolCall)
			}
		}
	}
}

func TestAnthropicAuthTokenUsesBearerNotAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "" {
			t.Error("AUTH_TOKEN must not set x-api-key")
		}
		if r.Header.Get("Authorization") != "Bearer auth-tok" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		beta := r.Header.Get("anthropic-beta")
		if strings.Contains(beta, "oauth-2025-04-20") {
			t.Errorf("AUTH_TOKEN must not add oauth betas, got %q", beta)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicFixture))
	}))
	defer srv.Close()

	client := &AnthropicClient{
		BaseURL: srv.URL, HTTPClient: srv.Client(),
		Headers: map[string]string{"Authorization": "Bearer auth-tok"},
	}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
}

func TestAnthropicOAuthTokenUsesBearerAndOAuthBetas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "" {
			t.Error("oauth token must not set x-api-key")
		}
		if r.Header.Get("Authorization") != "Bearer sk-ant-oat-xyz" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if !strings.Contains(r.Header.Get("anthropic-beta"), "oauth-2025-04-20") {
			t.Errorf("beta = %q", r.Header.Get("anthropic-beta"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicFixture))
	}))
	defer srv.Close()

	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "sk-ant-oat-xyz", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
}

func TestAnthropicClientHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
	}))
	defer srv.Close()

	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "bad", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{}, Options{Model: "claude-test"})
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	events, final := stream.Collect()
	if len(events) != 1 || events[0].Type != EventError {
		t.Fatalf("expected a single error event, got %v", events)
	}
	if final == nil || final.StopReason != StopError {
		t.Fatalf("final = %v, want stopReason=error", final)
	}
}

func TestAnthropicMaxTokensCompat(t *testing.T) {
	omit, send := false, true
	models.RegisterProvider(models.ProviderSpec{
		ID: "maxout-ant", DefaultAPI: "anthropic-messages", DefaultID: "unset",
		Models: []models.Model{
			{Provider: "maxout-ant", ID: "omit", Compat: &models.Compat{SupportsMaxOutputTokens: &omit}},
			{Provider: "maxout-ant", ID: "send", Compat: &models.Compat{SupportsMaxOutputTokens: &send}},
			{Provider: "maxout-ant", ID: "unset"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("maxout-ant") })

	cases := []struct {
		name, model string
		maxTokens   int
		thinking    string
		wantPresent bool
		want        int
	}{
		{name: "false omits", model: "omit", maxTokens: 100},
		{name: "false omits default", model: "omit"},
		{name: "false omits thinking bump", model: "omit", thinking: "high"},
		{name: "true sends", model: "send", maxTokens: 100, wantPresent: true, want: 100},
		{name: "unset sends default", model: "unset", wantPresent: true, want: defaultMaxTokens},
		{name: "unset thinking bump", model: "unset", thinking: "high", wantPresent: true, want: models.BudgetTokens("high") + 4096},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := buildAnthropicRequest(Context{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, Options{Provider: "maxout-ant", Model: tc.model, MaxTokens: tc.maxTokens, Thinking: tc.thinking})
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			got, ok := payload["max_tokens"]
			if !tc.wantPresent {
				if ok {
					t.Fatalf("unexpected max_tokens = %#v", got)
				}
				return
			}
			if !ok {
				t.Fatalf("missing max_tokens in %#v", payload)
			}
			n, _ := got.(float64)
			if int(n) != tc.want {
				t.Fatalf("max_tokens = %#v, want %d", got, tc.want)
			}
		})
	}
}

func TestAnthropicSessionAffinityHeaders(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "aff-ant", DefaultAPI: "anthropic-messages", DefaultID: "none",
		Models: []models.Model{
			{Provider: "aff-ant", ID: "on", Compat: &models.Compat{SendSessionAffinityHeaders: true}},
			{Provider: "aff-ant", ID: "off"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("aff-ant") })

	cases := []struct {
		name, provider, model, session, retention, want string
	}{
		{name: "compat true", provider: "aff-ant", model: "on", session: "sess-1", want: "sess-1"},
		{name: "compat unset", provider: "aff-ant", model: "off", session: "sess-1"},
		{name: "fireworks default", provider: "fireworks", model: "m", session: "sess-1", want: "sess-1"},
		{name: "fireworks cache none", provider: "fireworks", model: "m", session: "sess-1", retention: "none"},
		{name: "no session id", provider: "aff-ant", model: "on"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(anthropicFixture))
			}))
			defer srv.Close()

			client := &AnthropicClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
			stream, err := client.StreamFn()(context.Background(), Context{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, Options{Provider: tc.provider, Model: tc.model, SessionID: tc.session, CacheRetention: tc.retention})
			if err != nil {
				t.Fatal(err)
			}
			stream.Collect()
			if got.Get("x-session-affinity") != tc.want {
				t.Fatalf("x-session-affinity = %q, want %q", got.Get("x-session-affinity"), tc.want)
			}
		})
	}
}

func anthropicCacheFixture(startUsage, deltaUsage string) string {
	return fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-sonnet-4\",\"usage\":%s}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":%s}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n", startUsage, deltaUsage)
}

func TestAnthropicStreamPricesCatalogCost(t *testing.T) {
	body := anthropicCacheFixture(
		`{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":1000000}`,
		`{"output_tokens":0,"cache_creation_input_tokens":1000000,"cache_creation":{"ephemeral_1h_input_tokens":400000}}`,
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Provider: "anthropic", Model: "claude-sonnet-4"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil {
		t.Fatal("no final message")
	}
	if final.Usage.CacheWrite != 1_000_000 || final.Usage.CacheWrite1h != 400_000 {
		t.Fatalf("usage=%+v", final.Usage)
	}
	if math.Abs(final.Usage.Cost.CacheWrite-4.65) > 1e-9 || math.Abs(final.Usage.Cost.Total-4.65) > 1e-9 {
		t.Fatalf("cost=%+v, want cache write 4.65", final.Usage.Cost)
	}
}

func TestAnthropicCacheWriteTTLSplit(t *testing.T) {
	sonnet := &models.Cost{Input: 3, Output: 15, CacheRead: 0.30, CacheWrite: 3.75}
	cases := []struct {
		name       string
		start      string
		delta      string
		cacheWrite int
		cache1h    int
		writeCost  float64
	}{
		{
			name:       "five minutes only",
			start:      `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":1000000,"cache_creation":{"ephemeral_5m_input_tokens":1000000,"ephemeral_1h_input_tokens":0}}`,
			delta:      `{"output_tokens":1}`,
			cacheWrite: 1_000_000,
			cache1h:    0,
			writeCost:  3.75,
		},
		{
			name:       "one hour only",
			start:      `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":400000,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":400000}}`,
			delta:      `{"output_tokens":1}`,
			cacheWrite: 400_000,
			cache1h:    400_000,
			writeCost:  2.4,
		},
		{
			name:       "both ttls",
			start:      `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":1000000,"cache_creation":{"ephemeral_5m_input_tokens":600000,"ephemeral_1h_input_tokens":400000}}`,
			delta:      `{"output_tokens":1}`,
			cacheWrite: 1_000_000,
			cache1h:    400_000,
			writeCost:  4.65,
		},
		{
			name:       "no ttl breakdown",
			start:      `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":1000000}`,
			delta:      `{"output_tokens":1}`,
			cacheWrite: 1_000_000,
			cache1h:    0,
			writeCost:  3.75,
		},
		{
			name:       "vercel delta breakdown",
			start:      `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":1000000}`,
			delta:      `{"output_tokens":1,"cache_creation_input_tokens":1000000,"cache_creation":{"ephemeral_5m_input_tokens":600000,"ephemeral_1h_input_tokens":400000}}`,
			cacheWrite: 1_000_000,
			cache1h:    400_000,
			writeCost:  4.65,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := StreamAnthropicReader(context.Background(), strings.NewReader(anthropicCacheFixture(tc.start, tc.delta)), "claude-sonnet-4", sonnet)
			_, final := stream.Collect()
			if final == nil {
				t.Fatal("no final message")
			}
			u := final.Usage
			if u.CacheWrite != tc.cacheWrite || u.CacheWrite1h != tc.cache1h {
				t.Fatalf("cache write=%d 1h=%d, want %d and %d", u.CacheWrite, u.CacheWrite1h, tc.cacheWrite, tc.cache1h)
			}
			if u.TotalTokens != tc.cacheWrite+1 {
				t.Fatalf("total=%d, want %d (1h is a subset)", u.TotalTokens, tc.cacheWrite+1)
			}
			if math.Abs(u.Cost.CacheWrite-tc.writeCost) > 1e-9 {
				t.Fatalf("cache write cost=%v, want %v", u.Cost.CacheWrite, tc.writeCost)
			}
			if math.Abs(u.Cost.Total-tc.writeCost-sonnet.Output/1_000_000) > 1e-9 {
				t.Fatalf("total cost=%v", u.Cost.Total)
			}
		})
	}
}
