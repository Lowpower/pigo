package session_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

// TestAgentToolsSessionEndToEnd ties Phases 1–4: run the agent loop with real
// tools, then persist the resulting transcript to a JSONL session file and
// reload it.
func TestAgentToolsSessionEndToEnd(t *testing.T) {
	work := t.TempDir()
	agentDir := t.TempDir()
	file := filepath.Join(work, "hello.txt")
	if err := os.WriteFile(file, []byte("secret-42"), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := scriptedReadThenText(file)
	reg := tools.Default()
	exec := agent.ToolFunc(func(ctx context.Context, c agent.ToolCall) (string, bool) {
		return reg.Execute(ctx, c.Name, c.Args)
	})
	reqCtx := ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "read hello.txt"}}, Tools: reg.AITools()}

	events := agent.Run(context.Background(), provider, reqCtx, exec, agent.Config{Model: "test"}).Collect()
	transcript := events[len(events)-1].Messages

	// Persist the transcript.
	mgr := session.New(work, agentDir)
	for _, msg := range transcript {
		switch msg.Role {
		case agent.RoleAssistant:
			if _, err := mgr.AppendMessage("assistant", msg.Assistant); err != nil {
				t.Fatal(err)
			}
		case agent.RoleToolResult:
			if _, err := mgr.AppendMessage("toolResult", map[string]any{
				"role": "toolResult", "toolName": msg.ToolName, "toolCallId": msg.ToolCallID,
				"content": msg.Text, "isError": msg.IsError,
			}); err != nil {
				t.Fatal(err)
			}
		default:
			if _, err := mgr.AppendMessage("user", map[string]any{"role": "user", "content": msg.Text}); err != nil {
				t.Fatal(err)
			}
		}
	}

	header, entries, err := session.Load(mgr.File())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if header.Version != session.CurrentVersion {
		t.Errorf("version = %d, want %d", header.Version, session.CurrentVersion)
	}
	if len(entries) != len(transcript) {
		t.Fatalf("entries = %d, want %d", len(entries), len(transcript))
	}

	// The tool-result entry carries the file contents that the read tool produced.
	found := false
	for _, e := range entries {
		if strings.Contains(string(e.Message), "secret-42") {
			found = true
		}
	}
	if !found {
		t.Error("session does not contain the tool result (file contents)")
	}

	// The final assistant message round-trips.
	var lastAssistant ai.AssistantMessage
	if err := json.Unmarshal(entries[len(entries)-1].Message, &lastAssistant); err != nil {
		t.Fatalf("unmarshal final assistant: %v", err)
	}
	if lastAssistant.Text() == "" {
		t.Error("final assistant message text did not round-trip")
	}
}

func scriptedReadThenText(path string) ai.StreamFn {
	msgs := []*ai.AssistantMessage{
		{
			Role: ai.RoleAssistant, StopReason: ai.StopToolUse,
			Content: []*ai.Content{{Type: ai.KindToolCall, ToolID: "t1", ToolName: "read", Arguments: map[string]any{"path": path}}},
		},
		{
			Role: ai.RoleAssistant, StopReason: ai.StopStop,
			Content: []*ai.Content{{Type: ai.KindText, Text: "The file has been read."}},
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
