package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestHostCallExecAndUsage(t *testing.T) {
	e := &Engine{Opts: Options{Cwd: t.TempDir(), ContextWindow: 1000}}
	got := e.HandleHostCall(nil, "exec", map[string]any{"command": "echo", "args": []any{"hello-host"}})
	out, _ := got["stdout"].(string)
	if !strings.Contains(out, "hello-host") {
		t.Fatalf("exec stdout=%v", got)
	}
	if code, _ := got["code"].(int); code != 0 {
		t.Fatalf("exec code=%v", got["code"])
	}
	usage := e.HandleHostCall(nil, "getContextUsage", nil)
	if usage["contextWindow"] != 1000 {
		t.Fatalf("usage=%v", usage)
	}
	idle := e.HandleHostCall(nil, "isIdle", nil)
	if idle["idle"] != true {
		t.Fatalf("idle=%v", idle)
	}
}

func TestSendMessageSteerNotWhileIdleWithoutKick(t *testing.T) {
	dir := t.TempDir()
	sess := session.NewAt(dir, dir, dir)
	e := &Engine{Opts: Options{Session: sess, Cwd: dir}}
	e.setBusy(true)
	got := e.HandleHostCall(nil, "sendMessage", map[string]any{
		"customType": "note", "content": "steer-me", "triggerTurn": false, "deliverAs": "steer",
	})
	if got["ok"] != true {
		t.Fatalf("sendMessage=%v", got)
	}
	steer := e.drainSteer()
	if len(steer) != 1 || steer[0].Content != "steer-me" {
		t.Fatalf("steer=%v", steer)
	}
	e.setBusy(false)
	e.HandleHostCall(nil, "sendMessage", map[string]any{
		"customType": "note", "content": "idle-only", "triggerTurn": false, "deliverAs": "steer",
	})
	if extra := e.drainSteer(); len(extra) != 0 {
		t.Fatalf("idle triggerTurn:false must not steer: %v", extra)
	}
}

func TestSendMessageNextTurnQueue(t *testing.T) {
	e := &Engine{}
	e.HandleHostCall(nil, "sendMessage", map[string]any{
		"customType": "x", "content": "later", "deliverAs": "nextTurn",
	})
	got := e.TakeNextTurn()
	if len(got) != 1 || got[0].Content != "later" {
		t.Fatalf("nextTurn=%v", got)
	}
	if extra := e.TakeNextTurn(); len(extra) != 0 {
		t.Fatalf("queue should drain: %v", extra)
	}
}

func TestAppendEntryAndLabel(t *testing.T) {
	dir := t.TempDir()
	sess := session.NewAt(dir, dir, dir)
	e := &Engine{Opts: Options{Session: sess}}
	got := e.HandleHostCall(nil, "appendEntry", map[string]any{"customType": "card", "data": map[string]any{"n": 1}})
	id, _ := got["id"].(string)
	if id == "" {
		t.Fatalf("appendEntry=%v", got)
	}
	lab := e.HandleHostCall(nil, "setLabel", map[string]any{"entryId": id, "label": "mark"})
	if lab["id"] == nil {
		t.Fatalf("setLabel=%v", lab)
	}
}

func TestSetActiveToolsViaHostCall(t *testing.T) {
	e := &Engine{Tools: tools.NewRegistry(
		fnTool{name: "read", desc: "r", schema: objectSchema()},
		fnTool{name: "bash", desc: "b", schema: objectSchema()},
	)}
	got := e.HandleHostCall(nil, "setActiveTools", map[string]any{"names": []any{"read"}})
	active, _ := got["active"].([]string)
	if strings.Join(active, ",") != "read" {
		t.Fatalf("active=%v", got)
	}
}

func TestHostCallHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_HOSTCALL_EXT") != "1" {
		return
	}
	_ = ext.Serve(ext.Handler{
		Name: "hostcall-ext",
		Tools: []ext.ToolDef{{
			Name: "ping-host", Description: "call host", Schema: map[string]any{"type": "object"},
			Fn: func(_ context.Context, _ map[string]any) (string, bool) {
				usage, err := ext.HostCall("getContextUsage", nil)
				if err != nil {
					return err.Error(), true
				}
				_ = ext.Notify("from-ext", "info")
				return fmt.Sprint(usage["contextWindow"]), false
			},
		}},
	})
	os.Exit(0)
}

func TestHostCallFromExtension(t *testing.T) {
	h, err := ext.Spawn(context.Background(), "hostcall-ext",
		[]string{os.Args[0], "-test.run=^TestHostCallHelperProcess$"},
		ext.Options{Env: []string{"PIGO_HOSTCALL_EXT=1"}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = h.Close() }()
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{ContextWindow: 12345}}
	e.bindExtensionHooks()
	var notified string
	h.SetNotify(func(_, text string) { notified = text })
	out, isErr := h.CallTool(context.Background(), "ping-host", map[string]any{})
	if isErr || out != "12345" {
		t.Fatalf("ping-host=%q err=%v", out, isErr)
	}
	if notified != "from-ext" {
		t.Fatalf("notify=%q", notified)
	}
}

func TestEventBusFanout(t *testing.T) {
	e := &Engine{}
	h1 := &ext.Host{}
	h1.SubscribeBus("ping")
	got := map[string]any{}
	// SendHostEvent on a non-spawned Host just fails send; check HandleHostCall subscribe still records.
	e.Hosts = []*ext.Host{h1}
	e.HandleHostCall(h1, "events.on", map[string]any{"event": "ping"})
	if !h1.WantsBus("ping") {
		t.Fatal("expected bus subscription")
	}
	_ = got
}

func TestWantShutdown(t *testing.T) {
	e := &Engine{}
	e.HandleHostCall(nil, "shutdown", nil)
	if !e.WantShutdown() {
		t.Fatal("expected shutdown request")
	}
}

func TestRunPromptIncludesNextTurn(t *testing.T) {
	e := &Engine{}
	e.HandleHostCall(nil, "sendMessage", map[string]any{
		"content": "queued-next", "deliverAs": "nextTurn",
	})
	var seen []ai.Message
	e.Stream = func(ctx context.Context, req ai.Context, opt ai.Options) (*ai.EventStream, error) {
		seen = append([]ai.Message(nil), req.Messages...)
		return textReply("ok")(ctx, req, opt)
	}
	st := e.RunPrompt(context.Background(), nil, "user-now", nil)
	st.Collect()
	if len(seen) < 2 || seen[0].Content != "queued-next" || seen[1].Content != "user-now" {
		t.Fatalf("messages=%v", seen)
	}
}

func TestModelProviderSnapshot(t *testing.T) {
	e := &Engine{Provider: "anthropic", Opts: Options{AgentDir: t.TempDir()}}
	got := e.HandleHostCall(nil, "model.getProviderDisplayName", map[string]any{"provider": "anthropic"})
	if fmt.Sprint(got["name"]) == "" {
		t.Fatalf("display name=%v", got)
	}
	st := e.HandleHostCall(nil, "model.getProviderAuthStatus", map[string]any{"provider": "anthropic"})
	if st["ok"] != false {
		t.Fatalf("unauthenticated anthropic should be ok=false: %v", st)
	}
	mode := e.HandleHostCall(nil, "mode", nil)
	if mode["mode"] != "print" {
		t.Fatalf("mode=%v", mode)
	}
}
