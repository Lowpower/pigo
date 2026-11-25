package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/codescript"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/mcp"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
	"github.com/Lowpower/pigo/internal/toolsearch"
)

func TestToolSearchStaysOffAndDescriptionIsFixed(t *testing.T) {
	e, err := New(context.Background(), Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
		NoMCP:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	tool := findBuiltin(e, "tool_search")
	if tool == nil {
		t.Fatal("tool_search is not registered")
	}
	if tool.Description() != toolsearch.Description {
		t.Fatalf("description = %q", tool.Description())
	}
	if strings.Contains(tool.Description(), "github") || strings.Contains(tool.Description(), "mcp__") {
		t.Fatalf("description names a server: %q", tool.Description())
	}
	for _, name := range e.activeSnapshot() {
		if name == "tool_search" {
			t.Fatal("tool_search is enabled by default")
		}
	}
	e.SetActiveTools(append(e.activeSnapshot(), "tool_search"))
	for _, tool := range codescript.Callable(e.scriptEnv()) {
		if tool.Name == "tool_search" {
			t.Fatal("codemode scripts can call tool_search")
		}
	}
	before := tool.Description()
	e.mcp = mcp.New([]mcp.Server{{
		Name: "github",
		Config: mcp.Config{
			Enabled:     true,
			Description: "Search code on github",
			URL:         "http://127.0.0.1:9",
			Exposure:    "deferred",
		},
	}}, t.TempDir())
	if findBuiltin(e, "tool_search").Description() != before {
		t.Fatal("description changed when a server was added")
	}
}

func TestToolSearchLoadsDeferredAndSkipsHidden(t *testing.T) {
	e := engineFromHost(spawnLoadout(t, "exposures"))
	e.Tools = tools.NewRegistry(append(e.Tools.List(), searchTool{engine: e})...)
	e.SetActiveTools([]string{"tool_search"})
	if got := fmt.Sprint(namesOf(e.providerTools())); got != "[tool_search]" {
		t.Fatalf("before = %s", got)
	}
	out, isErr := searchTool{engine: e}.Execute(context.Background(), map[string]any{"query": "later"})
	if isErr {
		t.Fatalf("search: %s", out)
	}
	if !strings.Contains(out, "mcp") && !strings.Contains(out, "later") {
		t.Fatalf("out = %q", out)
	}
	if strings.Contains(out, "secret") {
		t.Fatalf("hidden tool returned: %q", out)
	}
	if got := fmt.Sprint(namesOf(e.providerTools())); got != "[later tool_search]" {
		t.Fatalf("after = %s", got)
	}
	out, isErr = searchTool{engine: e}.Execute(context.Background(), map[string]any{"query": "   "})
	if !isErr || out != "query must not be empty" {
		t.Fatalf("blank = %q err=%v", out, isErr)
	}
	out, isErr = searchTool{engine: e}.Execute(context.Background(), map[string]any{"query": "later", "limit": 1.5})
	if !isErr || out != "limit must be a positive integer" {
		t.Fatalf("limit = %q err=%v", out, isErr)
	}
}

func TestToolSearchDeclaresLoadedToolOnNextRequest(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		bodies = append(bodies, req)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(bodies) == 1 {
			_, _ = w.Write([]byte(searchLaterSSE()))
			return
		}
		_, _ = w.Write([]byte(textDoneSSE()))
	}))
	t.Cleanup(srv.Close)

	e := engineFromHost(spawnLoadout(t, "exposures"))
	e.Tools = tools.NewRegistry(append(e.Tools.List(), searchTool{engine: e})...)
	e.Provider = "fireworks"
	e.Opts.Config.Provider = "fireworks"
	e.Opts.Config.Model = "accounts/fireworks/models/deepseek-v4-flash-0731"
	e.Stream = (&ai.AnthropicClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}).StreamFn()
	e.SetActiveTools([]string{"tool_search"})

	events := e.RunPrompt(context.Background(), nil, "find later", nil).Collect()
	last := events[len(events)-1]
	if last.Type != "agent_end" {
		t.Fatalf("last = %s", last.Type)
	}
	var stamped []string
	for _, m := range last.Messages {
		if m.Role == "toolResult" && m.ToolName == "tool_search" {
			stamped = m.AddedToolNames
		}
	}
	if fmt.Sprint(stamped) != "[later]" {
		t.Fatalf("stamped = %v", stamped)
	}
	if len(bodies) < 2 {
		t.Fatalf("requests = %d", len(bodies))
	}
	first := chatToolNamesOf(t, bodies[0]["tools"])
	if fmt.Sprint(first) != "[tool_search]" {
		t.Fatalf("first tools = %v", first)
	}
	second := bodies[1]
	listed := chatToolNamesOf(t, second["tools"])
	if fmt.Sprint(listed) != "[tool_search later]" && fmt.Sprint(listed) != "[later tool_search]" {
		t.Fatalf("second tools = %v", listed)
	}
	late := toolObject(t, second["tools"], "later")
	if late["defer_loading"] != true {
		t.Fatalf("deferred = %#v", late)
	}
	if !requestHasToolReference(second, "later") {
		t.Fatalf("missing tool_reference in %#v", second["messages"])
	}
}

func TestToolSearchWaitsForDeferredServer(t *testing.T) {
	srv := httptest.NewServer(delayMCPServer(200 * time.Millisecond))
	t.Cleanup(srv.Close)
	mgr := mcp.New([]mcp.Server{{
		Name: "demo",
		Config: mcp.Config{
			URL: srv.URL, Enabled: true, Exposure: "deferred",
		},
	}}, t.TempDir())
	mgr.Start(context.Background())
	t.Cleanup(mgr.Close)
	e := &Engine{
		mcp:   mgr,
		Opts:  Options{Cwd: t.TempDir()},
		Tools: tools.NewRegistry(),
	}
	e.Tools = tools.NewRegistry(searchTool{engine: e})
	e.SetActiveTools([]string{"tool_search"})
	start := time.Now()
	out, isErr := searchTool{engine: e}.Execute(context.Background(), map[string]any{"query": "ping"})
	if isErr {
		t.Fatalf("search: %s", out)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("search did not wait for the deferred server")
	}
	if !strings.Contains(out, "mcp__demo__ping") {
		t.Fatalf("out = %q", out)
	}
	if got := fmt.Sprint(namesOf(e.providerTools())); !strings.Contains(got, "mcp__demo__ping") {
		t.Fatalf("tools = %s", got)
	}
}

func TestResumeKeepsDeferredToolWhenServerReconnects(t *testing.T) {
	srv := httptest.NewServer(boxMCPServer(0))
	t.Cleanup(srv.Close)
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	if _, err := sess.AppendActiveToolsChange([]string{"tool_search", "mcp__demo__ping"}); err != nil {
		t.Fatal(err)
	}
	mgr := mcp.New([]mcp.Server{{
		Name: "demo",
		Config: mcp.Config{
			URL: srv.URL, Enabled: true, Exposure: "deferred",
		},
	}}, cwd)
	mgr.Start(context.Background())
	t.Cleanup(mgr.Close)
	e := searchEngine(mgr, sess, cwd)
	e.applySessionToolPolicy(sess)
	if strings.Contains(fmt.Sprint(namesOf(e.providerTools())), "mcp__demo__ping") {
		t.Fatal("deferred tool was visible before the server was waited on")
	}
	if fmt.Sprint(e.pendingTools) != "[mcp__demo__ping]" {
		t.Fatalf("pending = %v", e.pendingTools)
	}
	e.settleLoadedTools(context.Background())
	if got := fmt.Sprint(namesOf(e.providerTools())); got != "[tool_search mcp__demo__ping]" {
		t.Fatalf("after settle = %s", got)
	}
	if len(e.pendingTools) != 0 {
		t.Fatalf("pending left = %v", e.pendingTools)
	}
}

func TestResumeDropsDeferredToolWhenServerFails(t *testing.T) {
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	if _, err := sess.AppendActiveToolsChange([]string{"tool_search", "mcp__down__ping"}); err != nil {
		t.Fatal(err)
	}
	mgr := mcp.New([]mcp.Server{{
		Name: "down",
		Config: mcp.Config{
			URL: "http://127.0.0.1:1", Enabled: true, Exposure: "deferred", TimeoutSeconds: 1,
		},
	}}, cwd)
	mgr.Start(context.Background())
	t.Cleanup(mgr.Close)
	e := searchEngine(mgr, sess, cwd)
	e.applySessionToolPolicy(sess)
	e.settleLoadedTools(context.Background())
	if strings.Contains(fmt.Sprint(namesOf(e.providerTools())), "mcp__down__ping") {
		t.Fatal("failed server tool stayed visible")
	}
	names, ok := session.LastActiveToolNames(sess.GetBranch(""))
	if !ok || fmt.Sprint(names) != "[tool_search]" {
		t.Fatalf("session tools = %v ok=%v", names, ok)
	}
}

func TestReloadHoldsLoadedToolUntilNextPrompt(t *testing.T) {
	srv := httptest.NewServer(boxMCPServer(0))
	t.Cleanup(srv.Close)
	cwd := t.TempDir()
	mgr := mcp.New([]mcp.Server{{
		Name: "demo",
		Config: mcp.Config{
			URL: srv.URL, Enabled: true, Exposure: "deferred",
		},
	}}, cwd)
	mgr.Start(context.Background())
	t.Cleanup(mgr.Close)
	if err := mgr.WaitEnabled(context.Background()); err != nil {
		t.Fatal(err)
	}
	e := searchEngine(mgr, nil, cwd)
	e.installMCPTools()
	e.SetActiveTools([]string{"tool_search", "mcp__demo__ping"})
	if !strings.Contains(fmt.Sprint(namesOf(e.providerTools())), "mcp__demo__ping") {
		t.Fatal("tool was not loaded")
	}
	prev := e.activeSnapshot()
	prevReg := map[string]bool{}
	for _, name := range registeredNames(e) {
		prevReg[name] = true
	}
	e.Tools = tools.NewRegistry(searchTool{engine: e})
	e.resetActiveToolsAfterReload(prev, prevReg, map[string]bool{})
	if got := fmt.Sprint(namesOf(e.providerTools())); !strings.Contains(got, "mcp__demo__ping") {
		t.Fatalf("reload dropped a connected tool: %s pending=%v", got, e.pendingTools)
	}
	if strings.Contains(findBuiltin(e, "tool_search").Description(), "demo") {
		t.Fatal("reload changed the tool_search description")
	}
}

func searchEngine(mgr *mcp.Manager, sess *session.Manager, cwd string) *Engine {
	e := &Engine{
		mcp:  mgr,
		Opts: Options{Session: sess, Cwd: cwd, Config: config.Config{}},
	}
	e.Tools = tools.NewRegistry(searchTool{engine: e})
	return e
}

func chatToolNamesOf(t *testing.T, raw any) []string {
	t.Helper()
	list, _ := raw.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		obj, _ := item.(map[string]any)
		name, _ := obj["name"].(string)
		out = append(out, name)
	}
	return out
}

func toolObject(t *testing.T, raw any, name string) map[string]any {
	t.Helper()
	list, _ := raw.([]any)
	for _, item := range list {
		obj, _ := item.(map[string]any)
		if obj["name"] == name {
			return obj
		}
	}
	t.Fatalf("missing tool %s", name)
	return nil
}

func searchLaterSSE() string {
	return strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","model":"fw","usage":{"input_tokens":1,"output_tokens":0}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"search1","name":"tool_search","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"later\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
}
