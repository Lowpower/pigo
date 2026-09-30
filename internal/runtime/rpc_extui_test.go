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

// rpcClient drives ServeRPC over an in-memory pipe.
type rpcClient struct {
	t    *testing.T
	pw   *io.PipeWriter
	enc  *json.Encoder
	out  *syncBuf
	done chan error
}

func startRPC(t *testing.T, e *Engine) *rpcClient {
	t.Helper()
	pr, pw := io.Pipe()
	c := &rpcClient{t: t, pw: pw, enc: json.NewEncoder(pw), out: &syncBuf{}, done: make(chan error, 1)}
	go func() { c.done <- e.ServeRPC(context.Background(), pr, c.out) }()
	c.waitRow("ready", 2*time.Second)
	return c
}

func (c *rpcClient) send(v map[string]any) {
	c.t.Helper()
	if err := c.enc.Encode(v); err != nil {
		c.t.Fatal(err)
	}
}

func (c *rpcClient) text() string { return c.out.String() }

func (c *rpcClient) rows() []map[string]any { return decodeRPCRows(c.t, c.text()) }

func (c *rpcClient) waitRow(typ string, timeout time.Duration) map[string]any {
	c.t.Helper()
	return c.waitMatch(typ, timeout, func(r map[string]any) bool { return r["type"] == typ })
}

func (c *rpcClient) waitCommand(command string, timeout time.Duration) {
	c.t.Helper()
	c.waitMatch("response to "+command, timeout, func(r map[string]any) bool {
		return r["type"] == "response" && r["command"] == command
	})
}

func (c *rpcClient) waitMatch(what string, timeout time.Duration, match func(map[string]any) bool) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, r := range c.rows() {
			if match(r) {
				return r
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.t.Fatalf("timeout waiting for %s in %s", what, c.text())
	return nil
}

// close sends quit, waits for ServeRPC to return and yields everything it wrote.
func (c *rpcClient) close() string {
	c.t.Helper()
	c.send(map[string]any{"type": "quit"})
	_ = c.pw.Close()
	select {
	case err := <-c.done:
		if err != nil {
			c.t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		c.t.Fatalf("ServeRPC did not return; out=%s", c.text())
	}
	return c.text()
}

func TestRPCExtensionUISelectRoundTrip(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	c := startRPC(t, e)

	got := make(chan map[string]any, 1)
	go func() {
		got <- e.RequestExtensionUI("select", map[string]any{
			"title":   "Allow dangerous command?",
			"options": []string{"Allow", "Block"},
		}, 2*time.Second)
	}()
	req := c.waitRow("extension_ui_request", 2*time.Second)
	if req["method"] != "select" || req["title"] != "Allow dangerous command?" {
		t.Fatalf("request = %#v", req)
	}
	id, _ := req["id"].(string)
	if id == "" {
		t.Fatalf("missing id in %#v", req)
	}
	c.send(map[string]any{"type": "extension_ui_response", "id": id, "value": "Allow"})
	select {
	case resp := <-got:
		if resp["value"] != "Allow" {
			t.Fatalf("response = %#v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dialog did not unblock")
	}
	c.close()
}

func TestRPCExtensionUIConfirmCancelled(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{}}}
	c := startRPC(t, e)
	got := make(chan map[string]any, 1)
	go func() {
		got <- e.RequestExtensionUI("confirm", map[string]any{
			"title":   "Clear session?",
			"message": "All messages will be lost.",
		}, 2*time.Second)
	}()
	req := c.waitRow("extension_ui_request", 2*time.Second)
	c.send(map[string]any{"type": "extension_ui_response", "id": req["id"], "cancelled": true})
	resp := <-got
	if resp["cancelled"] != true {
		t.Fatalf("response = %#v", resp)
	}
	c.close()
}

func TestRPCExtensionUITimeoutResolvesDefault(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{}}}
	c := startRPC(t, e)
	got := e.RequestExtensionUI("input", map[string]any{"title": "Enter a value"}, 50*time.Millisecond)
	req := c.waitRow("extension_ui_request", 2*time.Second)
	if req["method"] != "input" || req["timeout"] != float64(50) {
		t.Fatalf("request = %#v", req)
	}
	if _, ok := got["value"]; ok {
		t.Fatalf("timeout should not set value, got %#v", got)
	}
	if got["cancelled"] == true {
		t.Fatalf("timeout is not cancelled: %#v", got)
	}
	c.close()
}

func TestRPCExtensionUINotifyFireAndForget(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{}}}
	c := startRPC(t, e)
	start := time.Now()
	resp := e.RequestExtensionUI("notify", map[string]any{"message": "hi", "notifyType": "warning"}, 0)
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("notify should not block")
	}
	if resp != nil {
		t.Fatalf("notify resp = %#v", resp)
	}
	req := c.waitRow("extension_ui_request", 2*time.Second)
	if req["method"] != "notify" || req["message"] != "hi" {
		t.Fatalf("request = %#v", req)
	}
	c.close()
}
