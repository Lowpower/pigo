package ext

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
)

// TestExtHelperProcess is not a real test: when PIGO_EXT_HELPER=1 it runs as an
// extension subprocess (spawned by the tests below via the test binary itself).
func TestExtHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_EXT_HELPER") != "1" {
		return
	}
	_ = Serve(Handler{
		Name: "reverse-ext",
		Tools: []ToolDef{{
			Name:        "reverse",
			Description: "Reverse a string",
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
				"required":   []any{"text"},
			},
			Fn: func(_ context.Context, args map[string]any) (string, bool) {
				s, ok := args["text"].(string)
				if !ok {
					return "text must be a string", true
				}
				return reverseString(s), false
			},
		}},
	})
	os.Exit(0)
}

func spawnReverseExt(t *testing.T) *Host {
	t.Helper()
	h, err := Spawn(context.Background(), "reverse-ext",
		[]string{os.Args[0], "-test.run=^TestExtHelperProcess$"},
		Options{Env: []string{"PIGO_EXT_HELPER=1"}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	return h
}

func TestEnvCheckHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_EXT_HELPER") != "envcheck" {
		return
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		fmt.Fprintln(os.Stderr, "secret leaked")
		os.Exit(2)
	}
	_ = Serve(Handler{Name: "envcheck"})
	os.Exit(0)
}

func TestSpawnDropsSecretEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-reach-ext")
	h, err := Spawn(context.Background(), "envcheck",
		[]string{os.Args[0], "-test.run=^TestEnvCheckHelperProcess$"},
		Options{Env: []string{"PIGO_EXT_HELPER=envcheck", "ANTHROPIC_API_KEY=from-opts"}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	_ = h.Close()
}

func TestHostSpawnRegisterAndCall(t *testing.T) {
	h := spawnReverseExt(t)
	defer func() { _ = h.Close() }()

	tools := h.Tools()
	if len(tools) != 1 || tools[0].Name != "reverse" {
		t.Fatalf("tools = %+v, want one 'reverse'", tools)
	}
	if tools[0].Parameters["type"] != "object" {
		t.Errorf("reverse schema type = %v, want object", tools[0].Parameters["type"])
	}

	out, isErr := h.CallTool(context.Background(), "reverse", map[string]any{"text": "hello"})
	if isErr || out != "olleh" {
		t.Fatalf("reverse(hello) = %q (isErr=%v), want olleh", out, isErr)
	}

	out, isErr = h.CallTool(context.Background(), "does-not-exist", nil)
	if !isErr {
		t.Errorf("unknown tool should error, got %q", out)
	}
}

func TestHostClosedAfterExit(t *testing.T) {
	h := spawnReverseExt(t)
	if err := h.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Calls after close must fail cleanly, not hang.
	out, isErr := h.CallTool(context.Background(), "reverse", map[string]any{"text": "x"})
	if !isErr {
		t.Errorf("call after close should error, got %q", out)
	}
}

// TestAgentLoopWithExtensionTool: a scripted provider calls the extension's
// `reverse` tool, the host forwards it to the subprocess, and the result flows
// back through the loop.
func TestAgentLoopWithExtensionTool(t *testing.T) {
	h := spawnReverseExt(t)
	defer func() { _ = h.Close() }()

	provider := scriptedReverseThenText()
	exec := agent.ToolFunc(func(ctx context.Context, c agent.ToolCall) (string, bool) {
		return h.CallTool(ctx, c.Name, c.Args)
	})
	reqCtx := ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "reverse pigo"}}, Tools: h.Tools()}

	events := agent.Run(context.Background(), provider, reqCtx, exec, agent.Config{Model: "test"}).Collect()

	var toolResult string
	for _, e := range events {
		if e.Type == agent.EventToolEnd && e.ToolName == "reverse" {
			toolResult = e.Result
		}
	}
	if toolResult != "ogip" {
		t.Fatalf("extension tool result = %q, want ogip", toolResult)
	}
	if events[len(events)-1].Type != agent.EventAgentEnd {
		t.Fatalf("last event = %q, want agent_end", events[len(events)-1].Type)
	}
}

func scriptedReverseThenText() ai.StreamFn {
	msgs := []*ai.AssistantMessage{
		{Role: ai.RoleAssistant, StopReason: ai.StopToolUse, Content: []*ai.Content{
			{Type: ai.KindToolCall, ToolID: "r1", ToolName: "reverse", Arguments: map[string]any{"text": "pigo"}},
		}},
		{Role: ai.RoleAssistant, StopReason: ai.StopStop, Content: []*ai.Content{
			{Type: ai.KindText, Text: "Reversed."},
		}},
	}
	var i int
	return func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		m := msgs[len(msgs)-1]
		if i < len(msgs) {
			m = msgs[i]
		}
		i++
		return ai.EmitMessage(ctx, m), nil
	}
}

func TestHostSetActiveToolsHook(t *testing.T) {
	h := spawnSearchExt(t)
	defer func() { _ = h.Close() }()

	var got []string
	h.SetActiveToolsHook(func(names []string) { got = append([]string(nil), names...) })
	out, isErr := h.CallTool(context.Background(), "tool_search", map[string]any{"query": "lookup"})
	if isErr || out != "Found lookup." {
		t.Fatalf("tool_search = %q err=%v", out, isErr)
	}
	if fmt.Sprint(got) != "[tool_search lookup]" {
		t.Fatalf("hook names = %v", got)
	}
}

func TestSearchExtHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_EXT_SEARCH") != "1" {
		return
	}
	_ = Serve(Handler{
		Name: "search-ext",
		Tools: []ToolDef{
			{
				Name:        "tool_search",
				Description: "Find tools",
				Schema:      map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
				Fn: func(_ context.Context, _ map[string]any) (string, bool) {
					_ = SetActiveTools([]string{"tool_search", "lookup"})
					return "Found lookup.", false
				},
			},
			{
				Name:        "lookup",
				Description: "Look up a key",
				Schema:      map[string]any{"type": "object", "properties": map[string]any{"key": map[string]any{"type": "string"}}},
				Fn: func(_ context.Context, args map[string]any) (string, bool) {
					return fmt.Sprintf("value for %v", args["key"]), false
				},
			},
		},
	})
	os.Exit(0)
}

func spawnSearchExt(t *testing.T) *Host {
	t.Helper()
	h, err := Spawn(context.Background(), "search-ext",
		[]string{os.Args[0], "-test.run=^TestSearchExtHelperProcess$"},
		Options{Env: []string{"PIGO_EXT_SEARCH=1"}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	return h
}

func reverseString(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}
