package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/mcp"
	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestOldSessionAppendsMCPSection(t *testing.T) {
	cwd := t.TempDir()
	agent := t.TempDir()
	sess := session.New(cwd, agent)
	off := false
	e := &Engine{
		Tools: tools.NewRegistry(),
		Opts:  Options{Session: sess, Cwd: cwd, AgentDir: agent, Config: config.Config{CompactionOn: &off}},
		mcp:   mcp.New(nil, cwd),
	}
	if !strings.Contains(e.syncSystem(""), "You are an expert coding assistant") {
		t.Fatal("missing preamble")
	}
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(sess.File())
	if err != nil {
		t.Fatal(err)
	}
	opened, err := session.Open(sess.File())
	if err != nil {
		t.Fatalf("old session: %v", err)
	}
	if strings.Contains(session.ReplaySystem(opened.GetBranch("")).Prompt, "called from codemode") {
		t.Fatal("old session already had an mcp section")
	}

	e.mcp = mcp.New([]mcp.Server{{
		Name:   "github",
		Config: mcp.Config{Enabled: true, Description: "Search code.", Command: "unused"},
	}}, cwd)
	afterPrompt := e.syncSystem("")
	if !strings.Contains(afterPrompt, "github: called from codemode scripts. Search code.") {
		t.Fatalf("prompt=%s", afterPrompt)
	}
	after, err := os.ReadFile(sess.File())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) {
		t.Fatal("session file was rewritten")
	}
	if bytes.Count(after, []byte(`"mcp_servers"`)) != 1 {
		t.Fatalf("mcp section writes=%d\n%s", bytes.Count(after, []byte(`"mcp_servers"`)), after)
	}
	replay := session.ReplaySystem(sess.GetBranch(""))
	if replay.Sections[prompt.SectionMCPServers] == "" || replay.Sections[prompt.SectionPreamble] == "" {
		t.Fatalf("sections=%v", replay.Sections)
	}
}

func TestCodemodeServerDoesNotDelayPrompt(t *testing.T) {
	srv := httptest.NewServer(delayMCPServer(2 * time.Second))
	t.Cleanup(srv.Close)
	mgr := mcp.New([]mcp.Server{{
		Name:   "slow",
		Config: mcp.Config{URL: srv.URL, Enabled: true},
	}}, t.TempDir())
	mgr.Start(context.Background())
	t.Cleanup(mgr.Close)

	off := false
	e := &Engine{
		mcp:   mgr,
		Tools: tools.NewRegistry(),
		Opts:  Options{Cwd: t.TempDir(), Config: config.Config{CompactionOn: &off}},
		Stream: func(context.Context, ai.Context, ai.Options) (*ai.EventStream, error) {
			return nil, fmt.Errorf("provider down")
		},
	}
	wireQueues(e)
	start := time.Now()
	_ = e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("codemode server delayed the prompt by %s", time.Since(start))
	}
}

func TestMCPStructuredContentAndDirectWait(t *testing.T) {
	srv := httptest.NewServer(boxMCPServer(250 * time.Millisecond))
	t.Cleanup(srv.Close)
	mgr := mcp.New([]mcp.Server{{
		Name: "web",
		Config: mcp.Config{
			URL: srv.URL, Enabled: true, Exposure: "direct", Description: "box",
		},
	}}, t.TempDir())
	mgr.Start(context.Background())
	t.Cleanup(mgr.Close)

	off := false
	var saw time.Time
	e := &Engine{
		mcp:   mgr,
		Tools: tools.NewRegistry(),
		Opts:  Options{Cwd: t.TempDir(), Config: config.Config{CompactionOn: &off}},
		Stream: func(context.Context, ai.Context, ai.Options) (*ai.EventStream, error) {
			saw = time.Now()
			return nil, fmt.Errorf("provider down")
		},
	}
	wireQueues(e)
	start := time.Now()
	_ = e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if saw.IsZero() {
		t.Fatal("provider was not called")
	}
	if saw.Sub(start) < 150*time.Millisecond {
		t.Fatalf("direct server did not delay the prompt: %s", saw.Sub(start))
	}
	box := &ext.ToolResult{}
	text, isErr, _ := e.runRegisteredTool(ext.WithOutcome(context.Background(), box), "mcp__web__box", map[string]any{"mode": "array"})
	if isErr || text != "ok" {
		t.Fatalf("text=%q err=%v", text, isErr)
	}
	if string(box.Structured) != `["a",1]` && string(box.Structured) != `["a",1.0]` {
		t.Fatalf("structured=%s", box.Structured)
	}
	nullBox := &ext.ToolResult{}
	text, isErr, _ = e.runRegisteredTool(ext.WithOutcome(context.Background(), nullBox), "mcp__web__box", map[string]any{"mode": "null"})
	if isErr || string(nullBox.Structured) != "null" {
		t.Fatalf("text=%q structured=%s err=%v", text, nullBox.Structured, isErr)
	}
	infos := e.ToolsInNamespace("web")
	var sawBox bool
	for _, info := range infos {
		if info.Name == "mcp__web__box" && info.Namespace == "mcp__web" {
			sawBox = true
		}
	}
	if !sawBox {
		t.Fatalf("namespace=%+v", infos)
	}
	if _, ok := e.LookupTool("mcp__web__box"); !ok {
		t.Fatal("lookup missed the tool")
	}
}

func delayMCPServer(delay time.Duration) http.Handler {
	return newRuntimeMCPServer(delay, false)
}

func boxMCPServer(delay time.Duration) http.Handler {
	return newRuntimeMCPServer(delay, true)
}

func newRuntimeMCPServer(delay time.Duration, withBox bool) http.Handler {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fake", Version: "0"}, nil)
	server.AddReceivingMiddleware(func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if delay > 0 && (method == "initialize" || method == "server/discover") {
				timer := time.NewTimer(delay)
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
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"mode": map[string]any{"type": "string"},
	}}
	server.AddTool(&sdkmcp.Tool{Name: "ping", Description: "ping", InputSchema: schema}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "pong"}}}, nil
	})
	if withBox {
		server.AddTool(&sdkmcp.Tool{Name: "box", Description: "structured", InputSchema: schema}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
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
			case "null":
				res.StructuredContent = json.RawMessage("null")
			}
			return res, nil
		})
	}
	return sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
}
