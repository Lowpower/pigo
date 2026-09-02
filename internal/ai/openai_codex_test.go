package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
)

func TestResolveCodexURL(t *testing.T) {
	if got := resolveCodexURL(""); got != "https://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("%s", got)
	}
	if got := resolveCodexURL("https://chatgpt.com/backend-api"); got != "https://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("%s", got)
	}
	if got := resolveCodexURL("https://example/codex"); got != "https://example/codex/responses" {
		t.Fatalf("%s", got)
	}
}

func TestOpenAICodexClientHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/codex/responses") {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("originator") != "pi" {
			t.Errorf("originator = %q", r.Header.Get("originator"))
		}
		if r.Header.Get("OpenAI-Beta") != "responses=experimental" {
			t.Errorf("beta = %q", r.Header.Get("OpenAI-Beta"))
		}
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Encoding") != "zstd" {
			t.Errorf("content-encoding = %q", r.Header.Get("Content-Encoding"))
		}
		dec, err := zstd.NewReader(nil)
		if err != nil {
			t.Fatal(err)
		}
		defer dec.Close()
		plain, err := dec.DecodeAll(body, nil)
		if err != nil {
			t.Fatalf("zstd: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(plain, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["store"] != false {
			t.Errorf("store = %v", payload["store"])
		}
		if payload["stream"] != true {
			t.Errorf("stream = %v", payload["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()

	client := &OpenAICodexClient{
		BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(), Transport: "sse",
		Headers: map[string]string{"originator": "pi", "OpenAI-Beta": "responses=experimental"},
	}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-5.3-codex-spark"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if final.API != "openai-codex-responses" {
		t.Fatalf("api = %q", final.API)
	}
}

func TestStreamForOpenAICodex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("chatgpt-account-id") != "acct-1" {
			t.Errorf("account = %q", r.Header.Get("chatgpt-account-id"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()
	stream, err := StreamFor("openai-codex", ClientConfig{
		APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(),
		Headers: map[string]string{"chatgpt-account-id": "acct-1"},
	})(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-5.3-codex-spark"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("%+v", final)
	}
}

func TestResolveCodexWebSocketURL(t *testing.T) {
	got := resolveCodexWebSocketURL("https://chatgpt.com/backend-api")
	if got != "wss://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("%s", got)
	}
	got = resolveCodexWebSocketURL("http://127.0.0.1:9")
	if got != "ws://127.0.0.1:9/codex/responses" {
		t.Fatalf("%s", got)
	}
}

func TestOpenAICodexClientWebSocket(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var gotType, gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("OpenAI-Beta")
		if r.Header.Get("accept") != "" || r.Header.Get("content-type") != "" {
			t.Errorf("ws handshake should omit sse content headers, accept=%q content-type=%q", r.Header.Get("accept"), r.Header.Get("content-type"))
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read: %v", err)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Errorf("json: %v", err)
			return
		}
		gotType, _ = payload["type"].(string)
		for _, ev := range []string{
			`{"type":"response.output_text.delta","item_id":"msg_1","delta":"Hello","content_index":0,"output_index":0,"sequence_number":1,"logprobs":[]}`,
			`{"type":"response.output_text.delta","item_id":"msg_1","delta":", world","content_index":0,"output_index":0,"sequence_number":2,"logprobs":[]}`,
			`{"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","status":"completed","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`,
		} {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(ev)); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	client := &OpenAICodexClient{
		BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(), Transport: "websocket",
		Headers: map[string]string{"originator": "pi"},
	}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-5.3-codex-spark"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if gotType != "response.create" {
		t.Fatalf("ws payload type = %q", gotType)
	}
	if gotBeta != "responses_websockets=2026-02-06" {
		t.Fatalf("ws OpenAI-Beta = %q", gotBeta)
	}
}

func TestOpenAICodexClientWebSocketFallsBackToSSE(t *testing.T) {
	var sawSSE bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "no websocket", http.StatusBadGateway)
			return
		}
		sawSSE = true
		if r.Header.Get("Content-Encoding") != "zstd" {
			t.Errorf("sse content-encoding = %q", r.Header.Get("Content-Encoding"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	t.Cleanup(srv.Close)

	client := &OpenAICodexClient{
		BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(), Transport: "auto",
		Headers: map[string]string{"originator": "pi", "OpenAI-Beta": "responses=experimental"},
	}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-5.3-codex-spark"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if !sawSSE {
		t.Fatal("expected sse fallback")
	}
}

func TestCodexWebSocketInputDelta(t *testing.T) {
	baseline := []any{
		map[string]any{"role": "user", "content": "a"},
	}
	current := []any{
		map[string]any{"role": "user", "content": "a"},
		map[string]any{"role": "assistant", "content": "b"},
		map[string]any{"role": "user", "content": "c"},
	}
	delta, ok := wsInputDelta(current, baseline)
	if !ok || len(delta) != 2 {
		t.Fatalf("delta=%v ok=%v", delta, ok)
	}
	if _, ok := wsInputDelta(current[:1], append(baseline, "x")); ok {
		t.Fatal("mismatch should fail")
	}
}

func TestOpenAICodexClientWebSocketConnectionLimitRetry(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		attempts++
		if attempts == 1 {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","code":"websocket_connection_limit_reached","message":"limit"}`))
			return
		}
		for _, ev := range []string{
			`{"type":"response.output_text.delta","item_id":"msg_1","delta":"Hello","content_index":0,"output_index":0,"sequence_number":1,"logprobs":[]}`,
			`{"type":"response.output_text.delta","item_id":"msg_1","delta":", world","content_index":0,"output_index":0,"sequence_number":2,"logprobs":[]}`,
			`{"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","status":"completed","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`,
		} {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(ev)); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(resetCodexWebSocketCache)

	client := &OpenAICodexClient{
		BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(), Transport: "websocket",
		Headers: map[string]string{"originator": "pi", "chatgpt-account-id": "acct-1"},
	}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-5.3-codex-spark", SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestOpenAICodexClientWebSocketReusesSession(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var upgrades int
	var secondPrev string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrades++
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var payload map[string]any
			_ = json.Unmarshal(data, &payload)
			if prev, _ := payload["previous_response_id"].(string); prev != "" {
				secondPrev = prev
			}
			for _, ev := range []string{
				`{"type":"response.output_text.delta","item_id":"msg_1","delta":"Hello","content_index":0,"output_index":0,"sequence_number":1,"logprobs":[]}`,
				`{"type":"response.output_text.delta","item_id":"msg_1","delta":", world","content_index":0,"output_index":0,"sequence_number":2,"logprobs":[]}`,
				`{"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","status":"completed","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`,
			} {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(ev)); err != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(resetCodexWebSocketCache)

	client := &OpenAICodexClient{
		BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(), Transport: "websocket",
		Headers: map[string]string{"originator": "pi", "chatgpt-account-id": "acct-1"},
	}
	run := func(msgs []Message) *AssistantMessage {
		stream, err := client.StreamFn()(context.Background(), Context{Messages: msgs}, Options{
			Model: "gpt-5.3-codex-spark", SessionID: "sess-reuse",
		})
		if err != nil {
			t.Fatal(err)
		}
		_, final := stream.Collect()
		return final
	}
	if got := run([]Message{{Role: RoleUser, Content: "hi"}}); got == nil || got.Text() != "Hello, world" {
		t.Fatalf("first = %v", got)
	}
	if got := run([]Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Assistant: &AssistantMessage{Role: RoleAssistant, Content: []*Content{{Type: KindText, Text: "Hello, world"}}}},
		{Role: RoleUser, Content: "again"},
	}); got == nil || got.Text() != "Hello, world" {
		t.Fatalf("second = %v", got)
	}
	if upgrades != 1 {
		t.Fatalf("upgrades = %d", upgrades)
	}
	if secondPrev != "resp_1" {
		t.Fatalf("previous_response_id = %q", secondPrev)
	}
}
