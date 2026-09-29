package session

import (
	"encoding/json"
	"fmt"
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

func TestRestoreAIMessagesAddedToolNames(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok", "stopReason": "toolUse"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("toolResult", map[string]any{
		"role": "toolResult", "toolCallId": "s1", "toolName": "tool_search",
		"content": "Found lookup.", "isError": false, "addedToolNames": []string{"lookup"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendActiveToolsChange([]string{"tool_search", "lookup"}); err != nil {
		t.Fatal(err)
	}
	msgs := RestoreAIMessages(m.Entries())
	if len(msgs) != 3 {
		t.Fatalf("len=%d", len(msgs))
	}
	if fmt.Sprint(msgs[2].AddedToolNames) != "[lookup]" {
		t.Fatalf("added = %#v", msgs[2].AddedToolNames)
	}
	names, ok := LastActiveToolNames(m.Entries())
	if !ok || fmt.Sprint(names) != "[tool_search lookup]" {
		t.Fatalf("active = %v ok=%v", names, ok)
	}
}

func TestNewWithIDAndExactLookup(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	m := NewWithID(cwd, agentDir, "exact-session", "")
	if m.ID() != "exact-session" {
		t.Fatalf("id=%s", m.ID())
	}
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	got, err := FindExactIDAt(cwd, agentDir, "exact-session", "")
	if err != nil || got.ID() != "exact-session" {
		t.Fatalf("exact = %v %v", got, err)
	}
	if _, err := FindExactIDAt(cwd, agentDir, "exact", ""); err == nil {
		t.Fatal("prefix must not match exact lookup")
	}
}

func TestInMemoryDoesNotWrite(t *testing.T) {
	cwd := t.TempDir()
	m := InMemory(cwd, "ephemeral-id")
	if m.ID() != "ephemeral-id" {
		t.Fatalf("id=%s", m.ID())
	}
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.File()); !os.IsNotExist(err) {
		t.Fatalf("in-memory session wrote %s: %v", m.File(), err)
	}
}

func TestRestoreAIMessagesToolResultBlocks(t *testing.T) {
	cases := []struct {
		name    string
		content any
		details any
		want    string
		images  int
		data    string
		mime    string
	}{
		{name: "string", content: "hello", want: "hello"},
		{
			name: "text blocks",
			content: []any{
				map[string]any{"type": "text", "text": "a"},
				map[string]any{"type": "text", "text": "b"},
			},
			want: "a\nb",
		},
		{
			name: "text and image",
			content: []any{
				map[string]any{"type": "text", "text": "look"},
				map[string]any{"type": "image", "data": "AAA", "mimeType": "image/png"},
			},
			want:   "look",
			images: 1,
			data:   "AAA",
			mime:   "image/png",
		},
		{name: "empty array", content: []any{}, want: ""},
		{
			name: "details ignored",
			content: []any{
				map[string]any{"type": "text", "text": "body"},
			},
			details: map[string]any{"extra": true},
			want:    "body",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{
				"role": "toolResult", "toolCallId": "c1", "toolName": "bash",
				"content": tc.content, "isError": false,
			}
			if tc.details != nil {
				payload["details"] = tc.details
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			msgs := RestoreAIMessages([]Entry{{Type: "message", Message: raw}})
			if len(msgs) != 1 {
				t.Fatalf("len=%d", len(msgs))
			}
			got := msgs[0]
			if got.Role != ai.RoleToolResult || got.ToolCallID != "c1" || got.ToolName != "bash" || got.IsError {
				t.Fatalf("message = %+v", got)
			}
			if got.Content != tc.want {
				t.Fatalf("content = %q, want %q", got.Content, tc.want)
			}
			if len(got.Images) != tc.images {
				t.Fatalf("images = %#v, want %d", got.Images, tc.images)
			}
			if tc.images == 1 && (got.Images[0].Data != tc.data || got.Images[0].MimeType != tc.mime) {
				t.Fatalf("image = %#v", got.Images[0])
			}
		})
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

func TestRestoreAIMessagesUserImages(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, agentDir)
	content := []any{
		map[string]any{"type": "text", "text": "look"},
		map[string]any{"type": "image", "data": "AAA", "mimeType": "image/png"},
	}
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": content}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	msgs := RestoreAIMessages(m.Entries())
	if len(msgs) != 2 {
		t.Fatalf("len=%d", len(msgs))
	}
	if msgs[0].Content != "look" || len(msgs[0].Images) != 1 || msgs[0].Images[0].Data != "AAA" {
		t.Fatalf("user = %+v", msgs[0])
	}
}
