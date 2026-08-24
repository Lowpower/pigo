package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/tools"
)

// TestAgentLoopWithRealTools ties Phase 2 (agent loop) to Phase 3 (real tools):
// a scripted provider asks to `read` a real file, the registry executes it, and
// the loop feeds the result back and finishes.
func TestAgentLoopWithRealTools(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(file, []byte("secret-contents-42"), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := scriptedReadThenText(file)

	reg := tools.Default()
	exec := agent.ToolFunc(func(ctx context.Context, call agent.ToolCall) (string, bool) {
		return reg.Execute(ctx, call.Name, call.Args)
	})

	reqCtx := ai.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "read hello.txt"}},
		Tools:    reg.AITools(),
	}
	events := agent.Run(context.Background(), provider, reqCtx, exec, agent.Config{Model: "test"}).Collect()

	// The tool actually ran and produced the file contents.
	var toolResult string
	for _, e := range events {
		if e.Type == agent.EventToolEnd && e.ToolName == "read" {
			toolResult = e.Result
		}
	}
	if !strings.Contains(toolResult, "secret-contents-42") {
		t.Fatalf("read tool result = %q, want file contents", toolResult)
	}

	last := events[len(events)-1]
	if last.Type != agent.EventAgentEnd {
		t.Fatalf("last event = %q, want agent_end", last.Type)
	}
	// Transcript ends with the assistant's final text turn.
	final := last.Messages[len(last.Messages)-1]
	if final.Role != agent.RoleAssistant || final.Assistant.Text() == "" {
		t.Fatalf("final message = %+v, want assistant text", final)
	}
}

// scriptedReadThenText returns a provider that requests read(path) on the first
// turn and returns text on the second.
func scriptedReadThenText(path string) ai.StreamFn {
	msgs := []*ai.AssistantMessage{
		{
			Role:       ai.RoleAssistant,
			StopReason: ai.StopToolUse,
			Content:    []*ai.Content{{Type: ai.KindToolCall, ToolID: "t1", ToolName: "read", Arguments: map[string]any{"path": path}}},
		},
		{
			Role:       ai.RoleAssistant,
			StopReason: ai.StopStop,
			Content:    []*ai.Content{{Type: ai.KindText, Text: "The file has been read."}},
		},
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
