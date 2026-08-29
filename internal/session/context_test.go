package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	_ = a
}
