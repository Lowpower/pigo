package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicStreamObservesRawEventBeforeNormalizedDelta(t *testing.T) {
	const fixture = "" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}},\"vendorNote\":\"keep-me\"}\n\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"},\"vendorNote\":\"raw-delta\"}\n\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, fixture)
	}))
	defer srv.Close()

	var raw []map[string]any
	client := &AnthropicClient{
		BaseURL:    srv.URL,
		APIKey:     "sk-provider-secret",
		HTTPClient: srv.Client(),
	}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{
		Model: "claude-test",
		OnProviderStreamEvent: func(data []byte) {
			if strings.Contains(string(data), "sk-provider-secret") {
				t.Errorf("provider event leaked api key: %s", data)
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				t.Errorf("provider event is not json: %s", data)
				return
			}
			raw = append(raw, m)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	events, final := stream.Collect()

	var delta map[string]any
	for _, m := range raw {
		if m["type"] == "content_block_delta" {
			delta = m
		}
		if m["type"] == "text_delta" {
			t.Fatalf("raw event was already normalized: %#v", m)
		}
	}
	if delta == nil || delta["vendorNote"] != "raw-delta" {
		t.Fatalf("raw delta = %#v", delta)
	}
	var sawText bool
	for _, ev := range events {
		if ev.Type == EventTextDelta && ev.Delta == "Hello" {
			sawText = true
		}
	}
	if !sawText {
		t.Fatalf("normalized events = %#v", events)
	}
	if final == nil || final.Text() != "Hello" {
		t.Fatalf("final = %#v", final)
	}
}

func TestObserveSSEBodyKeepsUnknownFields(t *testing.T) {
	var n int
	body := &sseObserveReadCloser{
		rc: io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hi\",\"vendorNote\":\"raw\"}\n\n")),
		observe: func(data []byte) {
			n++
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			if m["type"] != "response.output_text.delta" || m["vendorNote"] != "raw" {
				t.Fatalf("event = %#v", m)
			}
		},
	}
	if _, err := io.ReadAll(body); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("events = %d", n)
	}
}
