package session

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
)

func TestContextEditOmitLeavesRawHistoryAndUsage(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	user, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	asst := &ai.AssistantMessage{
		Role:    "assistant",
		Content: []*ai.Content{{Type: ai.KindText, Text: "ack"}},
		Usage:   ai.Usage{Input: 3, Output: 4, TotalTokens: 7},
	}
	if _, err := m.AppendMessage("assistant", asst); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendContextEdit(user.ID, nil); err != nil {
		t.Fatal(err)
	}

	raw := RestoreAIMessages(ContextEntries(m))
	if !containsText(raw, "secret") {
		t.Fatalf("raw history lost the target: %+v", raw)
	}
	model := ModelMessages(ContextEntries(m))
	if containsText(model, "secret") {
		t.Fatalf("omitted message still in model context: %+v", model)
	}
	if !containsText(model, "ack") {
		t.Fatalf("unrelated message dropped: %+v", model)
	}
	stored, ok := m.EntryByID(user.ID)
	if !ok || !strings.Contains(string(stored.Message), "secret") {
		t.Fatalf("stored entry changed: %+v", stored)
	}
	stats := CollectStats(m, nil, 0)
	if stats.UserMessages != 1 || stats.Tokens.Input != 3 || stats.Tokens.Output != 4 {
		t.Fatalf("stats changed: %+v", stats)
	}
	last := lastJSONLObject(t, m.File())
	if _, ok := last["replacement"]; !ok {
		t.Fatalf("replacement omitted: %v", last)
	}
	if last["type"] != "context_edit" || last["targetId"] != user.ID || last["replacement"] != nil {
		t.Fatalf("jsonl = %v", last)
	}
}

func TestContextEditStringBecomesOneTextBlock(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "go"})
	asst, err := m.AppendMessage("assistant", &ai.AssistantMessage{
		Role: "assistant",
		Content: []*ai.Content{
			{Type: ai.KindText, Text: "calling"},
			{Type: ai.KindToolCall, ToolID: "c1", ToolName: "read", Arguments: map[string]any{"path": "a"}},
		},
		Usage: ai.Usage{Input: 1, Output: 2, TotalTokens: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := m.AppendMessage("toolResult", map[string]any{
		"role": "toolResult", "toolCallId": "c1", "toolName": "read", "content": "file-body", "isError": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendContextEdit(asst.ID, json.RawMessage(`"replaced"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendContextEdit(tool.ID, json.RawMessage(`"tool-replaced"`)); err != nil {
		t.Fatal(err)
	}

	var assistant, result *ai.Message
	for _, msg := range ModelMessages(ContextEntries(m)) {
		cp := msg
		if cp.Assistant != nil {
			assistant = &cp
		}
		if cp.Role == ai.RoleToolResult {
			result = &cp
		}
	}
	if assistant == nil || assistant.Assistant == nil || len(assistant.Assistant.Content) != 1 {
		t.Fatalf("assistant = %+v", assistant)
	}
	block := assistant.Assistant.Content[0]
	if block.Type != ai.KindText || block.Text != "replaced" {
		t.Fatalf("block = %+v", block)
	}
	if assistant.Assistant.Usage.Input != 1 {
		t.Fatalf("usage metadata dropped: %+v", assistant.Assistant.Usage)
	}
	if result == nil || result.Content != "tool-replaced" || result.ToolCallID != "c1" || result.ToolName != "read" {
		t.Fatalf("tool result = %+v", result)
	}
	stored, _ := m.EntryByID(asst.ID)
	if !strings.Contains(string(stored.Message), "calling") || !strings.Contains(string(stored.Message), "read") {
		t.Fatalf("raw assistant rewritten: %s", stored.Message)
	}
}

func TestContextEditLatestOnBranchAndNavigation(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	user, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "original"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	first, err := m.AppendContextEdit(user.ID, json.RawMessage(`"one"`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendContextEdit(user.ID, json.RawMessage(`"two"`)); err != nil {
		t.Fatal(err)
	}
	if got := userContent(ModelMessages(m.GetBranch(""))); got != "two" {
		t.Fatalf("latest edit = %q", got)
	}
	if err := m.Branch(first.ID); err != nil {
		t.Fatal(err)
	}
	if got := userContent(ModelMessages(m.GetBranch(""))); got != "one" {
		t.Fatalf("earlier branch = %q", got)
	}
	if err := m.Branch(user.ID); err != nil {
		t.Fatal(err)
	}
	if got := userContent(ModelMessages(m.GetBranch(""))); got != "original" {
		t.Fatalf("before edits = %q", got)
	}
}

func TestContextEditDoesNotEmitItsOwnMessage(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	user, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"})
	before := len(RestoreAIMessages(ContextEntries(m)))
	if _, err := m.AppendContextEdit(user.ID, json.RawMessage(`"x"`)); err != nil {
		t.Fatal(err)
	}
	if got := len(ModelMessages(ContextEntries(m))); got != before {
		t.Fatalf("model messages = %d, want %d", got, before)
	}
}

func TestAppendCompactionNilBoundaryUsesOwnID(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	old, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "old"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	comp, err := m.AppendCompaction("sum", "", 9, CompactionMeta{RetainNone: true})
	if err != nil {
		t.Fatal(err)
	}
	if comp.FirstKeptEntryID != comp.ID {
		t.Fatalf("firstKept = %q, id = %q", comp.FirstKeptEntryID, comp.ID)
	}
	after, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "after"})
	ctx := ContextEntries(m)
	for _, e := range ctx {
		if e.ID == old.ID {
			t.Fatalf("retained entry before retain-none compaction: %+v", ctx)
		}
	}
	if ctx[0].ID != comp.ID || ctx[len(ctx)-1].ID != after.ID {
		t.Fatalf("ctx ids = %v", entryIDs(ctx))
	}
	b := mustRead(t, m.File())
	if !strings.Contains(b, `"firstKeptEntryId":"`+comp.ID+`"`) {
		t.Fatalf("jsonl missing own-id boundary:\n%s", b)
	}
	if strings.Contains(b, `"firstKeptEntryId":null`) {
		t.Fatalf("retain-none stored JSON null:\n%s", b)
	}
}

func containsText(msgs []ai.Message, needle string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Text(), needle) || strings.Contains(m.Content, needle) {
			return true
		}
	}
	return false
}

func userContent(msgs []ai.Message) string {
	for _, m := range msgs {
		if m.Role == ai.RoleUser && m.Assistant == nil {
			return m.Content
		}
	}
	return ""
}

func entryIDs(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
