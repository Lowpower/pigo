package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/config"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func startRPC(t *testing.T, e *Engine) (*io.PipeWriter, *syncBuf, chan error) {
	t.Helper()
	pr, pw := io.Pipe()
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() { done <- e.ServeRPC(context.Background(), pr, out) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range decodeRPCRows(t, out.String()) {
			if r["type"] == "ready" {
				return pw, out, done
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("rpc never became ready")
	return nil, nil, nil
}

func waitRowType(t *testing.T, out *syncBuf, typ string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, r := range decodeRPCRows(t, out.String()) {
			if r["type"] == typ {
				return r
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s in %s", typ, out.String())
	return nil
}

func TestRPCExtensionUISelectRoundTrip(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	pw, out, done := startRPC(t, e)
	enc := json.NewEncoder(pw)

	got := make(chan map[string]any, 1)
	go func() {
		got <- e.RequestExtensionUI("select", map[string]any{
			"title":   "Allow dangerous command?",
			"options": []string{"Allow", "Block"},
		}, 2*time.Second)
	}()
	req := waitRowType(t, out, "extension_ui_request", 2*time.Second)
	if req["method"] != "select" || req["title"] != "Allow dangerous command?" {
		t.Fatalf("request = %#v", req)
	}
	id, _ := req["id"].(string)
	if id == "" {
		t.Fatalf("missing id in %#v", req)
	}
	if err := enc.Encode(map[string]any{"type": "extension_ui_response", "id": id, "value": "Allow"}); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-got:
		if resp["value"] != "Allow" {
			t.Fatalf("response = %#v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dialog did not unblock")
	}
	_ = enc.Encode(map[string]any{"type": "quit"})
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRPCExtensionUIConfirmCancelled(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{}}}
	pw, out, done := startRPC(t, e)
	enc := json.NewEncoder(pw)
	got := make(chan map[string]any, 1)
	go func() {
		got <- e.RequestExtensionUI("confirm", map[string]any{
			"title":   "Clear session?",
			"message": "All messages will be lost.",
		}, 2*time.Second)
	}()
	req := waitRowType(t, out, "extension_ui_request", 2*time.Second)
	if err := enc.Encode(map[string]any{"type": "extension_ui_response", "id": req["id"], "cancelled": true}); err != nil {
		t.Fatal(err)
	}
	resp := <-got
	if resp["cancelled"] != true {
		t.Fatalf("response = %#v", resp)
	}
	_ = enc.Encode(map[string]any{"type": "quit"})
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRPCExtensionUITimeoutResolvesDefault(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{}}}
	pw, out, done := startRPC(t, e)
	got := e.RequestExtensionUI("input", map[string]any{"title": "Enter a value"}, 50*time.Millisecond)
	req := waitRowType(t, out, "extension_ui_request", 2*time.Second)
	if req["method"] != "input" || req["timeout"] != float64(50) {
		t.Fatalf("request = %#v", req)
	}
	if _, ok := got["value"]; ok {
		t.Fatalf("timeout should not set value, got %#v", got)
	}
	if got["cancelled"] == true {
		t.Fatalf("timeout is not cancelled: %#v", got)
	}
	_ = json.NewEncoder(pw).Encode(map[string]any{"type": "quit"})
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRPCExtensionUINotifyFireAndForget(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{}}}
	pw, out, done := startRPC(t, e)
	start := time.Now()
	resp := e.RequestExtensionUI("notify", map[string]any{"message": "hi", "notifyType": "warning"}, 0)
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("notify should not block")
	}
	if resp != nil {
		t.Fatalf("notify resp = %#v", resp)
	}
	req := waitRowType(t, out, "extension_ui_request", 2*time.Second)
	if req["method"] != "notify" || req["message"] != "hi" {
		t.Fatalf("request = %#v", req)
	}
	_ = json.NewEncoder(pw).Encode(map[string]any{"type": "quit"})
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
