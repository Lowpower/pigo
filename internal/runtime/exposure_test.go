package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/skills"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestLoadoutHelperProcess(_ *testing.T) {
	kind := os.Getenv("PIGO_LOADOUT_EXT")
	if kind == "" {
		return
	}
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	switch kind {
	case "exposures":
		_ = ext.Serve(ext.Handler{
			Name: "exposures",
			Tools: []ext.ToolDef{
				{Name: "direct", Description: "direct tool", Schema: schema, Exposure: "direct", Aliases: []string{"dir"}, ReachesTools: []string{"later"}},
				{Name: "model", Description: "model only", Schema: schema, Exposure: "model-only", Namespace: "docs"},
				{Name: "code", Description: "code only", Schema: schema, Exposure: "codemode"},
				{Name: "later", Description: "deferred tool", Schema: schema, Exposure: "deferred"},
				{Name: "secret", Description: "hidden tool", Schema: schema, Exposure: "hidden"},
				{Name: "plain", Description: "old tool", Schema: schema, Annotations: map[string]any{"readOnly": true}},
				{Name: "read", Description: "read a file", Schema: schema, Exposure: "hidden"},
			},
		})
	case "prepare":
		_ = ext.Serve(ext.Handler{
			Name: "prepare",
			Tools: []ext.ToolDef{
				{Name: "direct", Description: "direct tool", Schema: schema, Exposure: "direct"},
				{Name: "model", Description: "model only", Schema: schema, Exposure: "model-only"},
				{Name: "secret", Description: "hidden tool", Schema: schema, Exposure: "hidden"},
			},
			PrepareLoadout: func(_ []string) ([]string, string) {
				return []string{"direct"}, "USE THE DIRECT TOOL"
			},
		})
	case "nested":
		_ = ext.Serve(ext.Handler{
			Name: "nested",
			Tools: []ext.ToolDef{
				{
					Name: "parent", Description: "calls child", Schema: schema,
					Fn: func(ctx context.Context, _ map[string]any) (string, bool) {
						text, isErr, err := ext.ExecuteTool("child", map[string]any{"k": "v"}, ext.ToolCallID(ctx))
						if err != nil {
							return err.Error(), true
						}
						if isErr {
							return text, true
						}
						return "parent:" + text, false
					},
				},
				{
					Name: "child", Description: "child", Schema: schema,
					Run: func(context.Context, map[string]any) ext.ToolResult {
						return ext.ToolResult{Text: "child-ok", Usage: &ai.Usage{Input: 3, Output: 4, TotalTokens: 7}}
					},
				},
				{
					Name: "secret", Description: "hidden", Schema: schema, Exposure: "hidden",
					Fn: func(context.Context, map[string]any) (string, bool) { return "nope", false },
				},
			},
		})
	default:
		fmt.Fprintf(os.Stderr, "unknown loadout helper %q\n", kind)
		os.Exit(2)
	}
	os.Exit(0)
}

func spawnLoadout(t *testing.T, kind string) *ext.Host {
	t.Helper()
	h, err := ext.Spawn(context.Background(), "loadout-ext",
		[]string{os.Args[0], "-test.run=^TestLoadoutHelperProcess$"},
		ext.Options{Env: []string{"PIGO_LOADOUT_EXT=" + kind}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func engineFromHost(h *ext.Host) *Engine {
	var ts []tools.Tool
	for _, info := range h.ToolInfos() {
		ts = append(ts, wrapExt(h, info.Name, info.Description, info.Schema))
	}
	e := &Engine{
		Hosts: []*ext.Host{h},
		Tools: tools.NewRegistry(ts...),
		Opts:  Options{Config: config.Config{Model: "x", Provider: "mock"}},
	}
	e.bindExtensionHooks()
	return e
}

func TestExtensionExposureFiltersModelTools(t *testing.T) {
	e := engineFromHost(spawnLoadout(t, "exposures"))
	if got := fmt.Sprint(toolNamesOf(e.providerTools())); got != "[direct model plain]" {
		t.Fatalf("model tools = %s", got)
	}
	e.rebuildSystemPrompt()
	if strings.Contains(e.System, "hidden tool") || strings.Contains(e.System, "code only") || strings.Contains(e.System, "deferred tool") {
		t.Fatalf("hidden tools leaked into prompt:\n%s", e.System)
	}
	if !strings.Contains(e.System, "direct tool") || !strings.Contains(e.System, "model only") || !strings.Contains(e.System, "old tool") {
		t.Fatalf("visible tools missing:\n%s", e.System)
	}
	e.Skills = []skills.Skill{{Name: "demo", Description: "Do the demo", FilePath: "/tmp/SKILL.md"}}
	e.rebuildSystemPrompt()
	if strings.Contains(e.System, "available_skills") || strings.Contains(e.System, "read a file") {
		t.Fatalf("hidden read leaked into skills prompt:\n%s", e.System)
	}
	e.LoadDeferred([]string{"later", "secret", "plain"})
	if got := fmt.Sprint(toolNamesOf(e.providerTools())); got != "[direct model later plain]" {
		t.Fatalf("after load = %s", got)
	}
	if info, ok := e.LookupTool("dir"); !ok || info.Name != "direct" {
		t.Fatalf("alias lookup = %+v ok=%v", info, ok)
	}
	ns := e.ToolsInNamespace("docs")
	if len(ns) != 1 || ns[0].Name != "model" {
		t.Fatalf("namespace = %+v", ns)
	}
	if got := fmt.Sprint(e.ReachesTools("dir")); got != "[later]" {
		t.Fatalf("reaches = %s", got)
	}
	plain := toolByName(e.providerTools(), "plain")
	if plain.Annotations["readOnly"] != true {
		t.Fatalf("annotations = %#v", plain.Annotations)
	}

	added := e.SetActiveTools([]string{"secret", "code", "direct"})
	if added != nil {
		t.Fatalf("hidden replace added = %v", added)
	}
	if got := fmt.Sprint(toolNamesOf(e.providerTools())); got != "[direct]" {
		t.Fatalf("whitelist = %s", got)
	}
	e.SetActiveTools([]string{"later"})
	if got := fmt.Sprint(toolNamesOf(e.providerTools())); got != "[later]" {
		t.Fatalf("loaded via active = %s", got)
	}
}

func TestPrepareLoadoutNarrowsRequestAndAddsGuidelines(t *testing.T) {
	e := engineFromHost(spawnLoadout(t, "prepare"))
	var seenSys string
	var seen []string
	e.Stream = func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		seenSys = req.System
		seen = toolNamesOf(req.Tools)
		return textReply("ok")(ctx, req, opts)
	}
	e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if fmt.Sprint(seen) != "[direct]" {
		t.Fatalf("request tools = %v", seen)
	}
	if !strings.Contains(seenSys, "USE THE DIRECT TOOL") {
		t.Fatalf("system = %q", seenSys)
	}
	if strings.Contains(e.System, "USE THE DIRECT TOOL") {
		t.Fatalf("guidelines were stored on the durable prompt: %q", e.System)
	}
}

func TestExecuteToolParentEventAndUsage(t *testing.T) {
	e := engineFromHost(spawnLoadout(t, "nested"))
	var events []agent.Event
	ctx := agent.WithToolEmitter(context.Background(), func(ev agent.Event) bool {
		events = append(events, ev)
		return true
	})
	out, isErr := e.Executor().Execute(ctx, agent.ToolCall{ID: "call-1", Name: "parent"})
	if isErr || out != "parent:child-ok" {
		t.Fatalf("out=%q isErr=%v", out, isErr)
	}
	meta := e.takeResultMeta(agent.ToolCall{ID: "call-1"})
	if meta.Usage == nil || meta.Usage.Input != 3 || meta.Usage.Output != 4 || meta.Usage.TotalTokens != 7 {
		t.Fatalf("usage = %+v", meta.Usage)
	}
	if meta.Nested == nil || !meta.Nested.Complete || len(meta.Nested.Calls) != 1 {
		t.Fatalf("nested = %+v", meta.Nested)
	}
	call := meta.Nested.Calls[0]
	if call.ID != "call-1/1" || call.Name != "child" || call.Status != "ok" {
		t.Fatalf("call = %+v", call)
	}
	var sawParent bool
	for _, ev := range events {
		if ev.Type == agent.EventToolStart && ev.ParentToolCallID == "call-1" && ev.ToolCallID == "call-1/1" {
			sawParent = true
		}
	}
	if !sawParent {
		t.Fatalf("events = %+v", events)
	}

	e.openNested(context.Background(), "call-2")
	hidden := e.executeNested(context.Background(), "call-2", "secret", nil)
	if hidden["isError"] != true {
		t.Fatalf("hidden = %v", hidden)
	}
	e.finishNested("call-2")
}

func TestRunPromptPersistsNestedUsage(t *testing.T) {
	e := engineFromHost(spawnLoadout(t, "nested"))
	cwd := t.TempDir()
	e.Opts.Cwd = cwd
	e.Opts.Session = session.New(cwd, t.TempDir())
	var n int
	e.Stream = func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		n++
		if n == 1 {
			return ai.EmitMessage(ctx, &ai.AssistantMessage{
				Role:       ai.RoleAssistant,
				StopReason: ai.StopToolUse,
				Content: []*ai.Content{{
					Type: ai.KindToolCall, ToolID: "call-1", ToolName: "parent",
					Arguments: map[string]any{},
				}},
			}), nil
		}
		return textReply("done")(ctx, req, opts)
	}
	e.RunPrompt(context.Background(), nil, "go", nil).Collect()
	msgs := session.RestoreAIMessages(e.Opts.Session.Entries())
	var found bool
	for _, m := range msgs {
		if m.ToolName != "parent" {
			continue
		}
		found = true
		if m.Content != "parent:child-ok" || m.Usage == nil || m.Usage.Input != 3 || m.Usage.Output != 4 {
			t.Fatalf("result = %+v", m)
		}
		if m.NestedCalls == nil || !m.NestedCalls.Complete || len(m.NestedCalls.Calls) != 1 || m.NestedCalls.Calls[0].ID != "call-1/1" {
			t.Fatalf("nested = %+v", m.NestedCalls)
		}
	}
	if !found {
		t.Fatal("parent tool result was not persisted")
	}
}

func TestNestedCallOmitsOversizedArguments(t *testing.T) {
	e := &Engine{Tools: tools.NewRegistry(fnTool{
		name: "echo", schema: objectSchema(),
		fn: func(context.Context, map[string]any) (string, bool) { return "ok", false },
	})}
	e.openNested(context.Background(), "parent")
	blob := strings.Repeat("a", maxNestedArgBytes+1)
	res := e.executeNested(context.Background(), "parent", "echo", map[string]any{"blob": blob})
	if res["isError"] == true {
		t.Fatalf("call = %v", res)
	}
	snap := e.finishNested("parent")
	if snap.calls == nil || snap.calls.Complete || len(snap.calls.Calls) != 1 {
		t.Fatalf("record = %+v", snap.calls)
	}
	call := snap.calls.Calls[0]
	if call.Arguments != nil || call.ArgumentsBytes == 0 || call.Status != "ok" {
		t.Fatalf("call = %+v", call)
	}
}

func TestNestedCallCapStops(t *testing.T) {
	e := &Engine{Tools: tools.NewRegistry(fnTool{
		name: "echo", schema: objectSchema(),
		fn: func(context.Context, map[string]any) (string, bool) { return "ok", false },
	})}
	e.openNested(context.Background(), "parent")
	for i := 0; i < maxNestedCalls; i++ {
		res := e.executeNested(context.Background(), "parent", "echo", map[string]any{"n": i})
		if res["isError"] == true {
			t.Fatalf("call %d: %v", i, res)
		}
	}
	res := e.executeNested(context.Background(), "parent", "echo", nil)
	if res["isError"] != true || res["content"] != "nested tool call limit exceeded" {
		t.Fatalf("cap = %v", res)
	}
	snap := e.finishNested("parent")
	if snap.calls == nil || snap.calls.Complete || len(snap.calls.Calls) != maxNestedCalls {
		n := 0
		complete := false
		if snap.calls != nil {
			n = len(snap.calls.Calls)
			complete = snap.calls.Complete
		}
		t.Fatalf("calls=%d complete=%v", n, complete)
	}
}

func toolByName(tools []ai.Tool, name string) ai.Tool {
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	return ai.Tool{}
}
