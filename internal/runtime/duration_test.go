package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestDurationHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_DURATION_EXT") == "" {
		return
	}
	mark := os.Getenv("PIGO_MARK")
	write := func(kind string, payload map[string]any) {
		f, err := os.OpenFile(mark, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		raw, _ := json.Marshal(payload)
		_, _ = f.WriteString(kind + " " + string(raw) + "\n")
		_ = f.Close()
	}
	_ = ext.Serve(ext.Handler{
		Name:   "duration",
		Events: []string{"tool_call", "tool_execution_end"},
		OnEvent: func(event string, payload map[string]any) map[string]any {
			if event == "tool_call" {
				if name, _ := payload["toolName"].(string); name == "nope" {
					return map[string]any{"block": true, "reason": "nope"}
				}
				return nil
			}
			write("end", payload)
			return nil
		},
		OnHostEvent: func(name string, args map[string]any) map[string]any {
			if name == "render.tool" {
				write("render", args)
				return map[string]any{"text": "rendered"}
			}
			return nil
		},
	})
}

func spawnDurationExt(t *testing.T, mark string) *ext.Host {
	t.Helper()
	h, err := ext.Spawn(context.Background(), "duration-ext",
		[]string{os.Args[0], "-test.run=^TestDurationHelperProcess$"},
		ext.Options{Env: []string{"PIGO_DURATION_EXT=1", "PIGO_MARK=" + mark}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func durationLines(t *testing.T, mark string) []string {
	t.Helper()
	raw, err := os.ReadFile(mark)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func linePayload(t *testing.T, line, kind string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(line, kind+" ") {
		t.Fatalf("line %q, want %s", line, kind)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, kind+" ")), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func durationEqual(payload map[string]any, ms *int) bool {
	got, ok := payload["durationMs"]
	if ms == nil {
		return !ok
	}
	return ok && got == float64(*ms)
}

func TestExecutorDurationMatchesEndEvent(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "events")
	h := spawnDurationExt(t, mark)
	var ran bool
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Config: config.Config{Model: "x", Provider: "mock"}},
	}
	e.Tools = tools.NewRegistry(fnTool{
		name: "echo", schema: objectSchema(),
		fn: func(context.Context, map[string]any) (string, bool) {
			ran = true
			return "ok", false
		},
	}, fnTool{
		name: "slow", schema: objectSchema(),
		fn: func(context.Context, map[string]any) (string, bool) {
			time.Sleep(30 * time.Millisecond)
			return "later", false
		},
	}, fnTool{
		name: "bad", schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"n": map[string]any{"type": "number"},
			},
			"required": []any{"n"},
		},
		fn: func(context.Context, map[string]any) (string, bool) {
			t.Fatal("invalid arguments reached the tool body")
			return "", true
		},
	}, fnTool{
		name: "parent", schema: objectSchema(),
		fn: func(ctx context.Context, _ map[string]any) (string, bool) {
			e.executeNested(ctx, ext.ToolCallID(ctx), "echo", map[string]any{})
			return "parent", false
		},
	})

	exec := e.Executor()
	if _, isErr := exec.Execute(context.Background(), agent.ToolCall{ID: "fast", Name: "echo", Args: map[string]any{}}); isErr || !ran {
		t.Fatalf("echo isErr=%v ran=%v", isErr, ran)
	}
	fast := e.takeResultMeta(agent.ToolCall{ID: "fast"})
	lines := durationLines(t, mark)
	if len(lines) != 1 {
		t.Fatalf("events = %v", lines)
	}
	end := linePayload(t, lines[0], "end")
	if !durationEqual(end, fast.DurationMs) || fast.DurationMs == nil {
		t.Fatalf("event %v meta %v", end["durationMs"], fast.DurationMs)
	}

	if _, isErr := exec.Execute(context.Background(), agent.ToolCall{ID: "wait", Name: "slow", Args: map[string]any{}}); isErr {
		t.Fatal(isErr)
	}
	slow := e.takeResultMeta(agent.ToolCall{ID: "wait"})
	lines = durationLines(t, mark)
	if len(lines) != 2 {
		t.Fatalf("events = %v", lines)
	}
	end = linePayload(t, lines[1], "end")
	if slow.DurationMs == nil || *slow.DurationMs < 20 || !durationEqual(end, slow.DurationMs) {
		t.Fatalf("slow event %v meta %v", end["durationMs"], slow.DurationMs)
	}
	if *fast.DurationMs >= *slow.DurationMs {
		t.Fatalf("fast %d should be shorter than slow %d", *fast.DurationMs, *slow.DurationMs)
	}

	if _, isErr := exec.Execute(context.Background(), agent.ToolCall{ID: "miss", Name: "missing", Args: map[string]any{}}); !isErr {
		t.Fatal("unknown tool should be an error")
	}
	if meta := e.takeResultMeta(agent.ToolCall{ID: "miss"}); meta.DurationMs != nil {
		t.Fatalf("unknown tool duration = %d", *meta.DurationMs)
	}
	lines = durationLines(t, mark)
	end = linePayload(t, lines[len(lines)-1], "end")
	if _, ok := end["durationMs"]; ok || end["toolCallId"] != "miss" {
		t.Fatalf("unknown tool end = %v", end)
	}

	if _, isErr := exec.Execute(context.Background(), agent.ToolCall{ID: "bad", Name: "bad", Args: map[string]any{}}); !isErr {
		t.Fatal("invalid arguments should be an error")
	}
	if meta := e.takeResultMeta(agent.ToolCall{ID: "bad"}); meta.DurationMs != nil {
		t.Fatalf("invalid args duration = %d", *meta.DurationMs)
	}
	lines = durationLines(t, mark)
	end = linePayload(t, lines[len(lines)-1], "end")
	if _, ok := end["durationMs"]; ok || end["toolName"] != "bad" {
		t.Fatalf("invalid args end = %v", end)
	}

	before := len(durationLines(t, mark))
	out, isErr := exec.Execute(context.Background(), agent.ToolCall{ID: "block", Name: "nope", Args: map[string]any{}})
	if !isErr || out != "nope" {
		t.Fatalf("block = %q isErr=%v", out, isErr)
	}
	if meta := e.takeResultMeta(agent.ToolCall{ID: "block"}); meta.DurationMs != nil {
		t.Fatalf("blocked duration = %d", *meta.DurationMs)
	}
	if got := len(durationLines(t, mark)); got != before {
		t.Fatalf("blocked call emitted tool_execution_end, lines %d -> %d", before, got)
	}

	if _, isErr := exec.Execute(context.Background(), agent.ToolCall{ID: "outer", Name: "parent", Args: map[string]any{}}); isErr {
		t.Fatal(isErr)
	}
	lines = durationLines(t, mark)
	if len(lines) < before+2 {
		t.Fatalf("nested events = %v", lines[before:])
	}
	nestedEnd := linePayload(t, lines[before], "end")
	parentEnd := linePayload(t, lines[before+1], "end")
	snap := e.takeResultMeta(agent.ToolCall{ID: "outer"})
	if snap.Nested == nil || len(snap.Nested.Calls) != 1 {
		t.Fatalf("nested record = %+v", snap.Nested)
	}
	child := snap.Nested.Calls[0].DurationMs
	if !durationEqual(nestedEnd, &child) || nestedEnd["toolCallId"] != "outer/1" {
		t.Fatalf("nested end = %v, record %d", nestedEnd, child)
	}
	if !durationEqual(parentEnd, snap.DurationMs) || snap.DurationMs == nil || *snap.DurationMs < child {
		t.Fatalf("parent end = %v, meta %v, child %d", parentEnd["durationMs"], snap.DurationMs, child)
	}
}

func TestPersistTranscriptKeepsZeroDuration(t *testing.T) {
	dir := t.TempDir()
	e := &Engine{Opts: Options{Session: session.New(dir, t.TempDir())}}
	zero := 0
	e.PersistTranscript([]agent.Msg{
		{Role: agent.RoleUser, Text: "go"},
		{Role: agent.RoleToolResult, ToolCallID: "c1", ToolName: "bash", Text: "ok", DurationMs: &zero},
	})
	entries := e.Opts.Session.Entries()
	var raw string
	for _, entry := range entries {
		if strings.Contains(string(entry.Message), `"toolCallId":"c1"`) {
			raw = string(entry.Message)
		}
	}
	if !strings.Contains(raw, `"durationMs":0`) {
		t.Fatalf("session toolResult = %s", raw)
	}
	msgs := session.RestoreAIMessages(entries)
	var got *int
	for _, m := range msgs {
		if m.ToolCallID == "c1" {
			got = m.DurationMs
		}
	}
	if got == nil || *got != 0 {
		t.Fatalf("restored duration = %v", got)
	}
	back := agentMsgFromAI(ai.Message{Role: ai.RoleToolResult, Content: "ok", ToolCallID: "c1", ToolName: "bash", DurationMs: &zero})
	if back.DurationMs == nil || *back.DurationMs != 0 {
		t.Fatalf("agent message duration = %v", back.DurationMs)
	}
}

func TestToolRenderPayloadCarriesPadAndDuration(t *testing.T) {
	zero := 0
	call := toolRenderPayload("call", "bash", map[string]any{"cmd": "ls"}, "", nil, 0)
	if _, ok := call["durationMs"]; ok || call["outputPad"] != 0 {
		t.Fatalf("call payload = %v", call)
	}
	done := toolRenderPayload("result", "bash", nil, "ok", &zero, 1)
	if done["durationMs"] != 0 || done["outputPad"] != 1 {
		t.Fatalf("result payload = %v", done)
	}
}

func TestRenderToolSendsOutputPad(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "events")
	h := spawnDurationExt(t, mark)
	zero := 0
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Config: config.Config{OutputPad: &zero}},
	}
	e.HandleHostCall(h, "registerToolRenderer", map[string]any{"name": "bash"})
	if got := e.RenderTool("call", "bash", map[string]any{"cmd": "ls"}, "", nil); got != "rendered" {
		t.Fatalf("render = %q", got)
	}
	lines := durationLines(t, mark)
	if len(lines) != 1 {
		t.Fatalf("lines = %v", lines)
	}
	payload := linePayload(t, lines[0], "render")
	if _, ok := payload["durationMs"]; ok || payload["outputPad"] != float64(0) {
		t.Fatalf("call render = %v", payload)
	}

	ms := 12
	if got := e.RenderTool("result", "bash", map[string]any{}, "ok", &ms); got != "rendered" {
		t.Fatalf("render = %q", got)
	}
	lines = durationLines(t, mark)
	payload = linePayload(t, lines[len(lines)-1], "render")
	if payload["durationMs"] != float64(12) || payload["outputPad"] != float64(0) {
		t.Fatalf("result render = %v", payload)
	}

	e.Opts.Config.OutputPad = nil
	e.RenderTool("call", "bash", nil, "", nil)
	lines = durationLines(t, mark)
	payload = linePayload(t, lines[len(lines)-1], "render")
	if payload["outputPad"] != float64(1) {
		t.Fatalf("default pad = %v", payload["outputPad"])
	}
}
