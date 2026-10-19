package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestAnthropicStreamRecordsProviderThinkingLevel(t *testing.T) {
	registerMidConvoModel(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicFixture))
	}))
	defer srv.Close()

	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "test", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Provider: "ant-mid", Model: "claude", Thinking: "low"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.ProviderThinkingLevel != "low" {
		t.Fatalf("providerThinkingLevel = %#v", final)
	}

	stream, err = client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Provider: "ant-mid", Model: "claude", Thinking: "off"})
	if err != nil {
		t.Fatal(err)
	}
	_, final = stream.Collect()
	if final == nil || final.ProviderThinkingLevel != "" {
		t.Fatalf("off providerThinkingLevel = %#v", final)
	}
}

func TestAnthropicInputTransformationsPreferMessageDelta(t *testing.T) {
	fixture := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":1,"output_tokens":0},"input_transformations":[{"type":"thinking_dropped","path":"messages.1.content.0","reason":"prefix_binding_mismatch"}]}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1},"input_transformations":[{"type":"thinking_dropped","path":"messages.3.content.0","reason":"model_binding_mismatch"}]}

event: message_stop
data: {"type":"message_stop"}
`
	_, final := StreamAnthropicReader(context.Background(), strings.NewReader(fixture), "claude-test", nil).Collect()
	if final == nil || len(final.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", final)
	}
	got := final.Diagnostics[0]
	if got.Type != "anthropic_input_transformations" || got.Timestamp == 0 || got.Details == nil {
		t.Fatalf("diagnostic = %#v", got)
	}
	want := []InputTransformation{{Type: "thinking_dropped", Path: "messages.3.content.0", Reason: "model_binding_mismatch"}}
	if !reflect.DeepEqual(got.Details.Transformations, want) {
		t.Fatalf("transformations = %#v", got.Details.Transformations)
	}
}

func TestThinkingDropNoticeDedup(t *testing.T) {
	one := droppedThinkingMessage(1)
	if got := ThinkingDropNotice(one, nil); got != "Anthropic dropped 1 thinking block (details in session)" {
		t.Fatalf("first = %q", got)
	}
	same := droppedThinkingMessage(2)
	previous := droppedThinkingMessage(2)
	if got := ThinkingDropNotice(same, previous); got != "" {
		t.Fatalf("repeat = %q", got)
	}
	more := droppedThinkingMessage(3)
	if got := ThinkingDropNotice(more, previous); got != "Anthropic dropped 3 thinking blocks (details in session)" {
		t.Fatalf("growth = %q", got)
	}
}

func droppedThinkingMessage(n int) *AssistantMessage {
	transformations := make([]InputTransformation, n)
	for i := range transformations {
		transformations[i] = InputTransformation{Type: "thinking_dropped", Path: "messages.0.content.0", Reason: "prefix_binding_mismatch"}
	}
	return &AssistantMessage{
		Role: RoleAssistant,
		Diagnostics: []AssistantDiagnostic{{
			Type:    "anthropic_input_transformations",
			Details: &DiagnosticDetails{Transformations: transformations},
		}},
	}
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
	}, Options{Provider: "anthropic", Model: "claude-sonnet-4-5"})
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

func TestAnthropicCatalogAndFallbackCost(t *testing.T) {
	priced := &models.Cost{Input: 3, Output: 15, CacheRead: 0.30, CacheWrite: 3.75}
	opus := &models.Cost{Input: 15, Output: 75, CacheRead: 1.5, CacheWrite: 18.75}
	haiku := &models.Cost{Input: 1, Output: 5, CacheRead: 0.1, CacheWrite: 1.25}
	models.RegisterProvider(models.ProviderSpec{
		ID: "cost-ant", DefaultAPI: "anthropic-messages", DefaultID: "priced",
		Models: []models.Model{
			{Provider: "cost-ant", ID: "priced", Cost: priced},
			{Provider: "cost-ant", ID: "free"},
			{Provider: "cost-ant", ID: "opus", Cost: opus, Compat: &models.Compat{AllowedFallbackModels: []models.FallbackModel{
				{Model: "claude-haiku-4", Cost: haiku},
				{Model: "claude-free-fallback"},
			}}},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("cost-ant") })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		usage := r.Header.Get("X-Test-Usage")
		if usage == "" {
			usage = `{"input_tokens":1000000,"output_tokens":1000000}`
		}
		served := r.Header.Get("X-Test-Model")
		if served == "" {
			served = req.Model
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":%q,\"usage\":%s}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":%s}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n", served, usage, usage)
	}))
	defer srv.Close()

	run := func(model, served, usage string) Usage {
		t.Helper()
		client := &AnthropicClient{
			BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(),
			Headers: map[string]string{},
		}
		if served != "" {
			client.Headers["X-Test-Model"] = served
		}
		if usage != "" {
			client.Headers["X-Test-Usage"] = usage
		}
		stream, err := client.StreamFn()(context.Background(), Context{
			Messages: []Message{{Role: RoleUser, Content: "hi"}},
		}, Options{Provider: "cost-ant", Model: model})
		if err != nil {
			t.Fatal(err)
		}
		_, final := stream.Collect()
		if final == nil {
			t.Fatal("no final message")
		}
		return final.Usage
	}

	t.Run("priced input and output", func(t *testing.T) {
		u := run("priced", "", "")
		if math.Abs(u.Cost.Input-3) > 1e-9 || math.Abs(u.Cost.Output-15) > 1e-9 || math.Abs(u.Cost.Total-18) > 1e-9 {
			t.Fatalf("cost=%+v, want input 3 output 15 total 18", u.Cost)
		}
	})

	t.Run("missing catalog cost stays zero", func(t *testing.T) {
		u := run("free", "", "")
		if u.Cost != (UsageCost{}) {
			t.Fatalf("cost=%+v, want zeros", u.Cost)
		}
	})

	t.Run("cache read and write", func(t *testing.T) {
		u := run("priced", "", `{"input_tokens":1000000,"output_tokens":0,"cache_read_input_tokens":2000000,"cache_creation_input_tokens":500000}`)
		if math.Abs(u.Cost.Input-3) > 1e-9 || math.Abs(u.Cost.CacheRead-0.6) > 1e-9 || math.Abs(u.Cost.CacheWrite-1.875) > 1e-9 {
			t.Fatalf("cost=%+v", u.Cost)
		}
		if math.Abs(u.Cost.Total-(3+0.6+1.875)) > 1e-9 {
			t.Fatalf("total=%v", u.Cost.Total)
		}
	})

	t.Run("fallback model price", func(t *testing.T) {
		u := run("opus", "claude-haiku-4", "")
		if math.Abs(u.Cost.Input-1) > 1e-9 || math.Abs(u.Cost.Output-5) > 1e-9 || math.Abs(u.Cost.Total-6) > 1e-9 {
			t.Fatalf("cost=%+v, want haiku input 1 output 5 total 6", u.Cost)
		}
	})

	t.Run("fallback without cost stays zero", func(t *testing.T) {
		u := run("opus", "claude-free-fallback", "")
		if u.Cost != (UsageCost{}) {
			t.Fatalf("cost=%+v, want zeros", u.Cost)
		}
	})

	t.Run("same served model keeps request price", func(t *testing.T) {
		u := run("opus", "opus", "")
		if math.Abs(u.Cost.Total-90) > 1e-9 {
			t.Fatalf("cost=%+v, want opus total 90", u.Cost)
		}
	})

	t.Run("unknown served model keeps request price", func(t *testing.T) {
		u := run("opus", "claude-unknown", "")
		if math.Abs(u.Cost.Total-90) > 1e-9 {
			t.Fatalf("cost=%+v, want opus total 90", u.Cost)
		}
	})
}

func claudeCodeIdentityContext() Context {
	return Context{
		System: "pigo system",
		Messages: []Message{
			{Role: RoleUser, Content: "hi"},
			{Assistant: &AssistantMessage{Content: []*Content{
				{Type: KindToolCall, ToolID: "tu_read", ToolName: "read", Arguments: map[string]any{"path": "a"}},
				{Type: KindToolCall, ToolID: "tu_bash", ToolName: "bash", Arguments: map[string]any{"command": "ls"}},
				{Type: KindToolCall, ToolID: "tu_search", ToolName: "tool_search", Arguments: map[string]any{}},
			}}},
			{Role: RoleToolResult, ToolCallID: "tu_read", ToolName: "read", Content: "file"},
			{Role: RoleToolResult, ToolCallID: "tu_bash", ToolName: "bash", Content: "ok"},
			{Role: RoleToolResult, ToolCallID: "tu_search", ToolName: "tool_search", Content: "found", AddedToolNames: []string{"grep"}},
		},
		Tools: []Tool{
			{Name: "read", Description: "read", Parameters: map[string]any{"type": "object"}},
			{Name: "bash", Description: "bash", Parameters: map[string]any{"type": "object"}},
			{Name: "tool_search", Description: "search", Parameters: map[string]any{"type": "object"}},
			{Name: "grep", Description: "grep", Parameters: map[string]any{"type": "object"}},
		},
	}
}

func claudeCodeToolSSE(names ...string) string {
	var b strings.Builder
	b.WriteString("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
	for i, name := range names {
		fmt.Fprintf(&b, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tu_%d\",\"name\":%q,\"input\":{}}}\n\n", i, i, name)
		fmt.Fprintf(&b, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", i)
	}
	b.WriteString("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n")
	return b.String()
}

func captureAnthropicRequest(t *testing.T, client *AnthropicClient, reqCtx Context, opts Options, sse string) (map[string]any, http.Header, *AssistantMessage) {
	t.Helper()
	var body []byte
	var header http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	t.Cleanup(srv.Close)
	client.BaseURL = srv.URL
	client.HTTPClient = srv.Client()
	stream, err := client.StreamFn()(context.Background(), reqCtx, opts)
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("request body: %v\n%s", err, body)
	}
	return req, header, final
}

func anthropicWireNames(req map[string]any) (tools, uses, refs []string) {
	for _, raw := range anySlice(req["tools"]) {
		tool, _ := raw.(map[string]any)
		tools = append(tools, jsonString(tool["name"]))
	}
	for _, raw := range anySlice(req["messages"]) {
		msg, _ := raw.(map[string]any)
		collectAnthropicNames(msg["content"], &uses, &refs)
	}
	return tools, uses, refs
}

func collectAnthropicNames(content any, uses, refs *[]string) {
	for _, raw := range anySlice(content) {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch block["type"] {
		case "tool_use":
			*uses = append(*uses, jsonString(block["name"]))
		case "tool_reference":
			*refs = append(*refs, jsonString(block["tool_name"]))
		case "tool_result":
			collectAnthropicNames(block["content"], uses, refs)
		}
	}
}

func jsonString(v any) string {
	s, _ := v.(string)
	return s
}

func systemBlocks(t *testing.T, req map[string]any) []map[string]any {
	t.Helper()
	raw, ok := req["system"].([]any)
	if !ok {
		t.Fatalf("system = %#v", req["system"])
	}
	out := make([]map[string]any, len(raw))
	for i, item := range raw {
		block, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("system[%d] = %#v", i, item)
		}
		out[i] = block
	}
	return out
}

func TestAnthropicOAuthClaudeCodeIdentity(t *testing.T) {
	const identity = "You are Claude Code, Anthropic's official CLI for Claude."
	reqCtx := claudeCodeIdentityContext()
	opts := Options{Provider: "fireworks", Model: "claude-test"}
	sse := claudeCodeToolSSE("Read", "Bash", "tool_search", "UnknownTool")

	req, hdr, final := captureAnthropicRequest(t, &AnthropicClient{APIKey: "sk-ant-oat-xyz"}, reqCtx, opts, sse)
	if hdr.Get("User-Agent") != "claude-cli/2.1.280" {
		t.Errorf("User-Agent = %q", hdr.Get("User-Agent"))
	}
	if hdr.Get("x-app") != "cli" {
		t.Errorf("x-app = %q", hdr.Get("x-app"))
	}
	if hdr.Get("anthropic-dangerous-direct-browser-access") != "true" {
		t.Errorf("browser access = %q", hdr.Get("anthropic-dangerous-direct-browser-access"))
	}
	if !strings.Contains(hdr.Get("anthropic-beta"), "oauth-2025-04-20") {
		t.Errorf("beta = %q", hdr.Get("anthropic-beta"))
	}

	sys := systemBlocks(t, req)
	if len(sys) != 2 {
		t.Fatalf("system = %#v", req["system"])
	}
	if sys[0]["text"] != identity {
		t.Errorf("system[0] = %#v", sys[0])
	}
	if sys[1]["text"] != "pigo system" {
		t.Errorf("system[1] = %#v", sys[1])
	}
	if _, ok := sys[0]["cache_control"]; !ok {
		t.Errorf("identity missing cache_control: %#v", sys[0])
	}
	if _, ok := sys[1]["cache_control"]; !ok {
		t.Errorf("pigo system missing cache_control: %#v", sys[1])
	}

	tools, uses, refs := anthropicWireNames(req)
	if fmt.Sprint(tools) != "[Read Bash tool_search Grep]" {
		t.Errorf("tools = %v", tools)
	}
	if fmt.Sprint(uses) != "[Read Bash tool_search]" {
		t.Errorf("tool_use = %v", uses)
	}
	if fmt.Sprint(refs) != "[Grep]" {
		t.Errorf("tool_reference = %v", refs)
	}

	if final == nil {
		t.Fatal("no final message")
	}
	var got []string
	for _, call := range final.ToolCalls() {
		got = append(got, call.ToolName)
	}
	if fmt.Sprint(got) != "[read bash tool_search UnknownTool]" {
		t.Errorf("restored tools = %v", got)
	}
	if reqCtx.Tools[0].Name != "read" || reqCtx.Messages[1].Assistant.Content[0].ToolName != "read" {
		t.Fatalf("session tool names changed: %#v", reqCtx)
	}

	t.Run("keeps caller user agent", func(t *testing.T) {
		_, hdr, _ := captureAnthropicRequest(t, &AnthropicClient{
			APIKey:  "sk-ant-oat-xyz",
			Headers: map[string]string{"User-Agent": "custom-ua"},
		}, Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "claude-test"}, anthropicFixture)
		if hdr.Get("User-Agent") != "custom-ua" {
			t.Errorf("User-Agent = %q", hdr.Get("User-Agent"))
		}
		if hdr.Get("x-app") != "cli" {
			t.Errorf("x-app = %q", hdr.Get("x-app"))
		}
	})

	t.Run("authorization header token", func(t *testing.T) {
		req, hdr, _ := captureAnthropicRequest(t, &AnthropicClient{
			Headers: map[string]string{"Authorization": "Bearer sk-ant-oat-from-header"},
		}, Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "claude-test"}, anthropicFixture)
		if hdr.Get("Authorization") != "Bearer sk-ant-oat-from-header" {
			t.Errorf("Authorization = %q", hdr.Get("Authorization"))
		}
		if hdr.Get("x-api-key") != "" {
			t.Errorf("x-api-key = %q", hdr.Get("x-api-key"))
		}
		if hdr.Get("User-Agent") != "claude-cli/2.1.280" || hdr.Get("x-app") != "cli" {
			t.Errorf("headers UA=%q x-app=%q", hdr.Get("User-Agent"), hdr.Get("x-app"))
		}
		sys := systemBlocks(t, req)
		if len(sys) != 1 || sys[0]["text"] != identity {
			t.Fatalf("system = %#v", req["system"])
		}
	})

	t.Run("cache retention none skips cache control", func(t *testing.T) {
		req, _, _ := captureAnthropicRequest(t, &AnthropicClient{APIKey: "sk-ant-oat-xyz"}, Context{
			System:   "pigo system",
			Messages: []Message{{Role: RoleUser, Content: "hi"}},
		}, Options{Model: "claude-test", CacheRetention: "none"}, anthropicFixture)
		for i, block := range systemBlocks(t, req) {
			if _, ok := block["cache_control"]; ok {
				t.Fatalf("system[%d] has cache_control: %#v", i, block)
			}
		}
	})

	t.Run("identity without pigo system", func(t *testing.T) {
		req, _, _ := captureAnthropicRequest(t, &AnthropicClient{APIKey: "sk-ant-oat-xyz"}, Context{
			Messages: []Message{{Role: RoleUser, Content: "hi"}},
		}, Options{Model: "claude-test"}, anthropicFixture)
		sys := systemBlocks(t, req)
		if len(sys) != 1 || sys[0]["text"] != identity {
			t.Fatalf("system = %#v", req["system"])
		}
		if _, ok := sys[0]["cache_control"]; !ok {
			t.Fatalf("identity missing cache_control: %#v", sys[0])
		}
	})
}

func TestAnthropicNonOAuthSkipsClaudeCodeIdentity(t *testing.T) {
	reqCtx := claudeCodeIdentityContext()
	opts := Options{Provider: "fireworks", Model: "claude-test"}
	sse := claudeCodeToolSSE("Read", "tool_search")

	check := func(t *testing.T, client *AnthropicClient) {
		t.Helper()
		req, hdr, final := captureAnthropicRequest(t, client, reqCtx, opts, sse)
		if strings.HasPrefix(hdr.Get("User-Agent"), "claude-cli/") {
			t.Errorf("User-Agent = %q", hdr.Get("User-Agent"))
		}
		if hdr.Get("x-app") != "" || hdr.Get("anthropic-dangerous-direct-browser-access") != "" {
			t.Errorf("identity headers = %q %q", hdr.Get("x-app"), hdr.Get("anthropic-dangerous-direct-browser-access"))
		}
		if strings.Contains(hdr.Get("anthropic-beta"), "oauth-2025-04-20") {
			t.Errorf("beta = %q", hdr.Get("anthropic-beta"))
		}
		sys := systemBlocks(t, req)
		if len(sys) != 1 || sys[0]["text"] != "pigo system" {
			t.Fatalf("system = %#v", req["system"])
		}
		tools, uses, refs := anthropicWireNames(req)
		if fmt.Sprint(tools) != "[read bash tool_search grep]" {
			t.Errorf("tools = %v", tools)
		}
		if fmt.Sprint(uses) != "[read bash tool_search]" {
			t.Errorf("tool_use = %v", uses)
		}
		if fmt.Sprint(refs) != "[grep]" {
			t.Errorf("tool_reference = %v", refs)
		}
		var got []string
		for _, call := range final.ToolCalls() {
			got = append(got, call.ToolName)
		}
		if fmt.Sprint(got) != "[Read tool_search]" {
			t.Errorf("response tools = %v", got)
		}
	}

	t.Run("api key", func(t *testing.T) {
		check(t, &AnthropicClient{APIKey: "sk-ant-api-key"})
	})
	t.Run("bearer auth token", func(t *testing.T) {
		check(t, &AnthropicClient{Headers: map[string]string{"Authorization": "Bearer auth-tok"}})
	})
}
