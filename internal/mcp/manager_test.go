package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("PIGO_MCP_FAKE"); mode != "" {
		if err := runFake(mode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestStdioAndHTTPTools(t *testing.T) {
	ctx := context.Background()
	stdio := New([]Server{{
		Name: "demo-srv",
		Config: Config{
			Command:     os.Args[0],
			Env:         map[string]string{"PIGO_MCP_FAKE": "stdio"},
			Description: "stdio demo",
			Enabled:     true,
		},
	}}, "")
	stdio.Start(ctx)
	t.Cleanup(stdio.Close)
	if err := stdio.WaitFor(ctx, "demo-srv", nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(stdio.Tools()) == 0 {
		t.Fatalf("no tools: %+v", stdio.Statuses())
	}
	ping := toolNamed(t, stdio.Tools(), "mcp__demo_srv__ping")
	got, err := stdio.Call(ctx, "demo-srv", ping.Remote, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "pong" || got.IsError {
		t.Fatalf("stdio call=%+v", got)
	}

	var lists atomic.Int32
	var sawAuth atomic.Bool
	base := newFakeHandler(&lists, false)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer test" {
			sawAuth.Store(true)
		}
		base.ServeHTTP(w, r)
	}))
	t.Cleanup(httpSrv.Close)
	remote := New([]Server{{
		Name: "web",
		Config: Config{
			URL:         httpSrv.URL,
			Headers:     map[string]string{"Authorization": "Bearer test"},
			Description: "http demo",
			Enabled:     true,
			Exposure:    "direct",
		},
	}}, "")
	remote.Start(ctx)
	t.Cleanup(remote.Close)
	if err := remote.WaitDirect(ctx); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"array", "scalar", "null"} {
		res, err := remote.Call(ctx, "web", "box", map[string]any{"mode": mode})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("mode %s: %+v", mode, res)
		}
		switch mode {
		case "array":
			if string(res.Structured) != `["a",1]` && string(res.Structured) != `["a",1.0]` {
				t.Fatalf("array=%s", res.Structured)
			}
		case "scalar":
			if string(res.Structured) != `"ok"` {
				t.Fatalf("scalar=%s", res.Structured)
			}
		case "null":
			if string(res.Structured) != "null" {
				t.Fatalf("null=%q", res.Structured)
			}
		}
	}
	var sawResources bool
	for _, st := range remote.Statuses() {
		if st.Name == "web" && st.HasResources {
			sawResources = true
		}
	}
	if !sawResources {
		t.Fatalf("statuses=%+v", remote.Statuses())
	}
	if lists.Load() != 0 {
		t.Fatalf("resources/list calls=%d", lists.Load())
	}
	if !sawAuth.Load() {
		t.Fatal("http request missing Authorization header")
	}
	for _, tool := range remote.Tools() {
		if strings.Contains(tool.Description, "resources:") {
			t.Fatalf("tool list mentions resources: %s", tool.Description)
		}
	}
}

func TestNonDirectDoesNotBlockAndCloseStopsHandshake(t *testing.T) {
	ctx := context.Background()
	slow := New([]Server{{
		Name: "slow",
		Config: Config{
			Command: os.Args[0],
			Env:     map[string]string{"PIGO_MCP_FAKE": "slow"},
			Enabled: true,
		},
	}}, "")
	slow.Start(ctx)
	start := time.Now()
	if err := slow.WaitDirect(ctx); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("codemode server delayed WaitDirect by %s", time.Since(start))
	}
	done := make(chan struct{})
	go func() {
		slow.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not stop the handshake")
	}
}

func TestOperateOneServerWhileAnotherConnects(t *testing.T) {
	ctx := context.Background()
	m := New([]Server{
		{
			Name: "slow",
			Config: Config{
				Command: os.Args[0],
				Env:     map[string]string{"PIGO_MCP_FAKE": "slow"},
				Enabled: true,
			},
		},
		{
			Name: "fast",
			Config: Config{
				Command: os.Args[0],
				Env:     map[string]string{"PIGO_MCP_FAKE": "stdio"},
				Enabled: true,
			},
		},
	}, "")
	m.Start(ctx)
	t.Cleanup(m.Close)
	if err := m.SetEnabled("fast", false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	fast := statusNamed(m, "fast")
	for time.Now().Before(deadline) && fast.Status != "disabled" {
		time.Sleep(20 * time.Millisecond)
		fast = statusNamed(m, "fast")
	}
	if fast.Status != "disabled" {
		t.Fatalf("fast=%+v", fast)
	}
	if st := statusNamed(m, "slow"); st.Status != "connecting" {
		t.Fatalf("slow=%+v", st)
	}
	if err := m.SetEnabled("fast", true); err != nil {
		t.Fatal(err)
	}
	if err := m.Reconnect("fast"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && statusNamed(m, "fast").Status != "ready" {
		time.Sleep(20 * time.Millisecond)
	}
	if st := statusNamed(m, "fast"); st.Status != "ready" {
		t.Fatalf("fast after reconnect=%+v", st)
	}
	if st := statusNamed(m, "slow"); st.Status != "connecting" {
		t.Fatalf("slow changed while fast reconnected: %+v", st)
	}
}

func statusNamed(m *Manager, name string) Status {
	for _, st := range m.Statuses() {
		if st.Name == name {
			return st
		}
	}
	return Status{}
}

func TestToolSearchWaitsForNamedServer(t *testing.T) {
	ctx := context.Background()
	m := New([]Server{{
		Name: "demo-srv",
		Config: Config{
			Command: os.Args[0],
			Env:     map[string]string{"PIGO_MCP_FAKE": "stdio"},
			Enabled: true,
		},
	}}, "")
	m.Start(ctx)
	t.Cleanup(m.Close)
	start := time.Now()
	if err := m.WaitFor(ctx, "tool_search", map[string]any{"server": "demo-srv"}, nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("waited too long")
	}
	if len(m.Tools()) == 0 {
		t.Fatal("server was not connected")
	}
	untouched := time.Now()
	if err := m.WaitFor(ctx, "tool_search", map[string]any{"query": "nothing"}, nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(untouched) > 200*time.Millisecond {
		t.Fatal("unnamed search waited")
	}
}

func toolNamed(t *testing.T, tools []ToolSpec, name string) ToolSpec {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("missing %s in %#v", name, tools)
	return ToolSpec{}
}

func runFake(mode string) error {
	server := newFakeServer(mode == "slow", nil)
	return server.Run(context.Background(), &sdkmcp.StdioTransport{})
}

func newFakeHandler(lists *atomic.Int32, slow bool) http.Handler {
	server := newFakeServer(slow, lists)
	return sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
}

func newFakeServer(slow bool, lists *atomic.Int32) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fake", Version: "0"}, &sdkmcp.ServerOptions{
		Instructions: "From the server.\nMore",
		Capabilities: &sdkmcp.ServerCapabilities{Resources: &sdkmcp.ResourceCapabilities{}},
	})
	server.AddReceivingMiddleware(func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if lists != nil && method == "resources/list" {
				lists.Add(1)
			}
			if slow && (method == "initialize" || method == "server/discover") {
				timer := time.NewTimer(30 * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
				}
			}
			return next(ctx, method, req)
		}
	})
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode": map[string]any{"type": "string"},
		},
	}
	server.AddTool(&sdkmcp.Tool{Name: "ping", Description: "ping the server", InputSchema: schema}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "pong"}}}, nil
	})
	server.AddTool(&sdkmcp.Tool{Name: "box", Description: "structured result", InputSchema: schema}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Mode string `json:"mode"`
		}
		if req != nil && req.Params != nil {
			_ = json.Unmarshal(req.Params.Arguments, &args)
		}
		res := &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}
		switch args.Mode {
		case "array":
			res.StructuredContent = []any{"a", 1}
		case "scalar":
			res.StructuredContent = "ok"
		case "null":
			res.StructuredContent = json.RawMessage("null")
		}
		return res, nil
	})
	return server
}
