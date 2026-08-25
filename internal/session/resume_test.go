package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
)

func TestContinueRecentOpensLatest(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	a := New(cwd, agentDir)
	if _, err := a.AppendMessage("user", map[string]any{"role": "user", "content": "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	m, err := ContinueRecent(cwd, agentDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID() != a.ID() {
		t.Fatalf("continue id = %s, want %s", m.ID(), a.ID())
	}
	if _, err := os.Stat(m.File()); err != nil {
		t.Fatal(err)
	}
	list, err := List(cwd, agentDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || filepath.Base(list[0]) != filepath.Base(a.File()) {
		t.Fatalf("list = %v", list)
	}
}

func TestRestoreAIMessagesRoundTrip(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, agentDir)
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	asst := map[string]any{
		"role": "assistant",
		"content": []map[string]any{
			{"type": "text", "text": "calling"},
			{"type": "toolCall", "id": "c1", "name": "read", "arguments": map[string]any{"path": "a.txt"}},
		},
		"stopReason": "toolUse",
	}
	if _, err := m.AppendMessage("assistant", asst); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("toolResult", map[string]any{"role": "toolResult", "toolCallId": "c1", "toolName": "read", "content": "hello", "isError": false}); err != nil {
		t.Fatal(err)
	}
	msgs := RestoreAIMessages(m.Entries())
	if len(msgs) != 3 {
		t.Fatalf("len=%d msgs=%+v", len(msgs), msgs)
	}
	if msgs[1].Assistant == nil || len(msgs[1].Assistant.ToolCalls()) != 1 {
		t.Fatalf("assistant tool calls missing: %+v", msgs[1])
	}
	if msgs[2].ToolCallID != "c1" || msgs[2].Content != "hello" {
		t.Fatalf("tool result = %+v", msgs[2])
	}
	opened, err := FindByID(cwd, agentDir, m.ID()[:8])
	if err != nil {
		t.Fatal(err)
	}
	if opened.ID() != m.ID() {
		t.Fatalf("FindByID id=%s want %s", opened.ID(), m.ID())
	}
}

func TestRestoreAIMessagesBashExecution(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	code := 0
	if _, err := m.AppendMessage("bashExecution", map[string]any{
		"role": "bashExecution", "command": "printf hi", "output": "hi",
		"cancelled": false, "truncated": false, "exitCode": code,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("bashExecution", map[string]any{
		"role": "bashExecution", "command": "printf secret", "output": "secret",
		"cancelled": false, "truncated": false, "excludeFromContext": true,
	}); err != nil {
		t.Fatal(err)
	}
	msgs := RestoreAIMessages(m.Entries())
	if len(msgs) != 1 {
		t.Fatalf("len=%d %+v", len(msgs), msgs)
	}
	if msgs[0].Role != ai.RoleUser || !strings.Contains(msgs[0].Content, "Ran `printf hi`") {
		t.Fatalf("msg = %+v", msgs[0])
	}
}
