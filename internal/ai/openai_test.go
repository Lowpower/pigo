package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestOpenAIClientHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("authorization"), "Bearer ") {
			t.Errorf("missing Bearer authorization, got %q", r.Header.Get("authorization"))
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAIFixture))
	}))
	defer srv.Close()

	client := &OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "gpt-test"})
	if err != nil {
		t.Fatalf("StreamFn error: %v", err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" || final.StopReason != StopToolUse {
		t.Fatalf("final = %+v", final)
	}
}

func eventTypes(events []Event) []EventType {
	out := make([]EventType, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}
