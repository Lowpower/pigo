package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const responsesFixture = `event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","delta":"Hello","content_index":0,"output_index":0,"sequence_number":1,"logprobs":[]}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","delta":", world","content_index":0,"output_index":0,"sequence_number":2,"logprobs":[]}

event: response.completed
data: {"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","status":"completed","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}

`

func TestOpenAIResponsesClientHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("authorization"), "Bearer ") {
			t.Errorf("missing Bearer authorization, got %q", r.Header.Get("authorization"))
		}
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			t.Errorf("path = %q, want .../responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()

	client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-test"})
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
}

func TestBuildResponsesInputToolPair(t *testing.T) {
	items := buildResponsesInput(Context{Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindToolCall, ToolID: "c1", ToolName: "read", Arguments: map[string]any{"path": "a"}},
		}}},
		{Role: RoleToolResult, ToolCallID: "c1", Content: "ok"},
	}})
	if len(items) < 3 {
		t.Fatalf("items = %d", len(items))
	}
}
