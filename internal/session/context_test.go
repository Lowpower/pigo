package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
)

func TestContextEntriesKeepsTailFromFirstKeptEntryId(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	old, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "old"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok-old"})
	keep, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "keep"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok-keep"})
	comp, err := m.AppendCompaction("digest", keep.ID, 99)
	if err != nil {
		t.Fatal(err)
	}
	after, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "after"})
	if err != nil {
		t.Fatal(err)
	}

	ctx := ContextEntries(m)
	ids := make([]string, len(ctx))
	texts := make([]string, 0, len(ctx))
	for i, e := range ctx {
		ids[i] = e.ID
		if e.Type == "compaction" {
			if e.FirstKeptEntryID != keep.ID || e.TokensBefore == nil || *e.TokensBefore != 99 {
				t.Fatalf("compaction fields = %+v", e)
			}
			if e.Summary != "digest" {
				t.Fatalf("summary = %q", e.Summary)
			}
			if len(e.Message) != 0 {
				t.Fatalf("compaction should not nest a message blob: %s", e.Message)
			}
		}
		if s := userText(&e); s != "" {
			texts = append(texts, s)
		}
	}
	joined := strings.Join(texts, ",")
	if strings.Contains(joined, "old") {
		t.Fatalf("summarized history leaked: %s ids=%v", joined, ids)
	}
	if ids[0] != comp.ID {
		t.Fatalf("context should start at compaction, got %v", ids)
	}
	foundKeep := false
	for _, id := range ids {
		if id == keep.ID {
			foundKeep = true
		}
	}
	if !foundKeep {
		t.Fatalf("retained tail missing keep entry: %v", ids)
	}
	if ids[len(ids)-1] != after.ID {
		t.Fatalf("missing post-compaction entry: %v", ids)
	}
	_ = old
}

func TestRestoreAIMessagesUsesPiCompactionWrapper(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	keep, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "keep"})
	_, _ = m.AppendCompaction("checkpoint", keep.ID, 10)
	msgs := RestoreAIMessages(ContextEntries(m))
	if len(msgs) < 2 {
		t.Fatalf("msgs=%d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "<summary>") || !strings.Contains(msgs[0].Content, "checkpoint") {
		t.Fatalf("compaction text = %q", msgs[0].Content)
	}
	if msgs[1].Content != "keep" {
		t.Fatalf("tail = %q", msgs[1].Content)
	}
}

func TestRestoreCustomMessageAndSkipCustom(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendCustomEntry("pi.share", map[string]any{"id": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendCustomMessage("note", "injected", true); err != nil {
		t.Fatal(err)
	}
	msgs := RestoreAIMessages(ContextEntries(m))
	if len(msgs) != 1 || msgs[0].Content != "injected" {
		t.Fatalf("msgs=%+v", msgs)
	}
}

func TestPiCompactionJSONLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pi.jsonl")
	body := `{"type":"session","version":3,"id":"sid","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":"old"}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-01-01T00:00:02.000Z","message":{"role":"assistant","content":"ok"}}
{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-01-01T00:00:03.000Z","message":{"role":"user","content":"keep"}}
{"type":"compaction","id":"c1","parentId":"m3","timestamp":"2026-01-01T00:00:04.000Z","summary":"sum","firstKeptEntryId":"m3","tokensBefore":42}
{"type":"message","id":"m4","parentId":"c1","timestamp":"2026-01-01T00:00:05.000Z","message":{"role":"user","content":"next"}}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := ContextEntries(opened)
	if len(ctx) < 3 || ctx[0].Type != "compaction" || ctx[0].FirstKeptEntryID != "m3" {
		t.Fatalf("ctx=%+v", ctx)
	}
	msgs := RestoreAIMessages(ctx)
	var texts []string
	for _, msg := range msgs {
		texts = append(texts, msg.Content)
	}
	joined := strings.Join(texts, "|")
	if strings.Contains(joined, "old") {
		t.Fatalf("old leaked: %s", joined)
	}
	if !strings.Contains(joined, "sum") || !strings.Contains(joined, "keep") || !strings.Contains(joined, "next") {
		t.Fatalf("context = %s", joined)
	}
}

func TestAppendCompactionJSONLShape(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, agent)
	u, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	a, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"})
	if _, err := m.AppendCompaction("sum", u.ID, 7); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatal(err)
	}
	if last["type"] != "compaction" {
		t.Fatalf("type=%v", last["type"])
	}
	if last["summary"] != "sum" || last["firstKeptEntryId"] != u.ID {
		t.Fatalf("last=%v", last)
	}
	if _, ok := last["message"]; ok {
		t.Fatalf("unexpected message field: %v", last)
	}
	if _, ok := last["fromHook"]; ok {
		t.Fatalf("unset fromHook should be omitted: %v", last)
	}
	_ = a
}

func lastJSONLObject(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatal(err)
	}
	return last
}

func TestAppendCustomMessageJSONLHasNoSummary(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendCustomMessage("note", "injected", true); err != nil {
		t.Fatal(err)
	}
	last := lastJSONLObject(t, m.File())
	if last["type"] != "custom_message" || last["customType"] != "note" {
		t.Fatalf("last=%v", last)
	}
	if _, ok := last["summary"]; ok {
		t.Fatalf("custom_message must not write summary: %v", last)
	}
	if last["display"] != true {
		t.Fatalf("display=%v", last["display"])
	}
}

func TestAppendBranchSummaryFromHookIsTopLevel(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendBranchSummary("from-1", "branch-sum", true); err != nil {
		t.Fatal(err)
	}
	last := lastJSONLObject(t, m.File())
	if last["type"] != "branch_summary" || last["fromId"] != "from-1" || last["summary"] != "branch-sum" {
		t.Fatalf("last=%v", last)
	}
	if last["fromHook"] != true {
		t.Fatalf("fromHook should be top-level true: %v", last)
	}
	if details, ok := last["details"]; ok {
		t.Fatalf("fromHook must not be stuffed into details: %v", details)
	}
}

func TestAppendCompactionJSONLOptionalFields(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	u, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendCompaction("sum", u.ID, 7, CompactionMeta{
		Details:  map[string]any{"readFiles": []string{"a.go"}},
		Usage:    &ai.Usage{Input: 1, Output: 2, TotalTokens: 3},
		FromHook: true,
	}); err != nil {
		t.Fatal(err)
	}
	last := lastJSONLObject(t, m.File())
	if last["type"] != "compaction" || last["fromHook"] != true {
		t.Fatalf("last=%v", last)
	}
	details, _ := last["details"].(map[string]any)
	if details["readFiles"] == nil {
		t.Fatalf("details=%v", last["details"])
	}
	usage, _ := last["usage"].(map[string]any)
	if usage["input"] != float64(1) || usage["output"] != float64(2) {
		t.Fatalf("usage=%v", last["usage"])
	}
}
