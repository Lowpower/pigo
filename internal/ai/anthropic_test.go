package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	stream := StreamAnthropicReader(context.Background(), strings.NewReader(anthropicFixture), "claude-test")
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

func TestAnthropicClientHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicFixture))
	}))
	defer srv.Close()

	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "claude-test"})
	if err != nil {
		t.Fatalf("StreamFn returned error: %v", err)
	}

	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("final text = %v, want Hello, world", final)
	}
	if final.StopReason != StopToolUse {
		t.Errorf("stopReason = %q, want toolUse", final.StopReason)
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
