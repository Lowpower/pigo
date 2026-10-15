package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/session"
)

func TestCompactionKeepsCutOffToolResult(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "start"}); err != nil {
		t.Fatal(err)
	}
	appendToolAssistant(t, sess, toolUse("read", "a.go"))
	if _, err := sess.AppendMessage("toolResult", map[string]any{
		"role": "toolResult", "toolCallId": "read-a.go", "toolName": "read",
		"content": strings.Repeat("x", 4000),
	}); err != nil {
		t.Fatal(err)
	}
	tail := appendUserText(t, sess, "tail")

	var convo string
	var ends []map[string]any
	e := compactionEngine(sess, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		_, convo = captureSummaryRequest(t, req)
		return textReply("SUM")(ctx, req, opts)
	})
	e.SetOnSessionEvent(func(v any) {
		ev, _ := v.(map[string]any)
		if ev["type"] == "compaction_end" {
			ends = append(ends, ev)
		}
	})
	msgs := session.ModelMessages(session.ContextEntries(sess))
	out, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 900}, false)
	if err != nil {
		t.Fatal(err)
	}
	entry := lastCompaction(t, sess)
	if entry.FirstKeptEntryID != tail.ID || out.FirstKeptEntryID != tail.ID {
		t.Fatalf("kept entry=%s result=%s want %s", entry.FirstKeptEntryID, out.FirstKeptEntryID, tail.ID)
	}
	if !strings.Contains(convo, "start") || !strings.Contains(convo, "[Tool result]") {
		t.Fatalf("summary dropped the tool result: %s", convo)
	}
	if strings.Contains(convo, "\ntail") || strings.Contains(convo, "[User]: tail") {
		t.Fatalf("summary included the kept tail: %s", convo)
	}
	if len(ends) != 1 {
		t.Fatalf("compaction_end events = %d", len(ends))
	}
	result, _ := ends[0]["result"].(map[string]any)
	if result["firstKeptEntryId"] != tail.ID {
		t.Fatalf("compaction_end firstKept = %v", result["firstKeptEntryId"])
	}
}

func TestSessionBeforeCompactReportsCut(t *testing.T) {
	h := spawnRuntimeExt(t, "compact-id", nil)
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "start"}); err != nil {
		t.Fatal(err)
	}
	appendToolAssistant(t, sess, toolUse("read", "a.go"))
	if _, err := sess.AppendMessage("toolResult", map[string]any{
		"role": "toolResult", "toolCallId": "read-a.go", "content": strings.Repeat("x", 4000),
	}); err != nil {
		t.Fatal(err)
	}
	tail := appendUserText(t, sess, "tail")
	e := compactionEngine(sess, textReply("unused"))
	e.Hosts = []*ext.Host{h}
	msgs := session.ModelMessages(session.ContextEntries(sess))
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 900}, false); err != nil {
		t.Fatal(err)
	}
	entry := lastCompaction(t, sess)
	if !entry.FromHook || entry.Summary != tail.ID || entry.FirstKeptEntryID != tail.ID {
		t.Fatalf("entry summary=%q kept=%s fromHook=%v", entry.Summary, entry.FirstKeptEntryID, entry.FromHook)
	}
}

func TestCompactionNoLegalCutReportsFailure(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	h := spawnRuntimeExt(t, "compact-failed", nil, "PIGO_MARK="+mark)
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("toolResult", map[string]any{
		"role": "toolResult", "toolCallId": "c1", "content": strings.Repeat("x", 4000),
	}); err != nil {
		t.Fatal(err)
	}
	var ends []map[string]any
	e := compactionEngine(sess, textReply("NO"))
	e.Hosts = []*ext.Host{h}
	e.SetOnSessionEvent(func(v any) {
		ev, _ := v.(map[string]any)
		if ev["type"] == "compaction_end" {
			ends = append(ends, ev)
		}
	})
	msgs := session.ModelMessages(session.ContextEntries(sess))
	_, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 1}, false)
	if !errors.Is(err, compaction.ErrNoLegalCut) {
		t.Fatalf("err = %v", err)
	}
	for _, entry := range sess.GetBranch("") {
		if entry.Type == "compaction" {
			t.Fatal("compaction entry was appended")
		}
	}
	if len(ends) != 1 || ends[0]["errorMessage"] != compaction.ErrNoLegalCut.Error() || ends[0]["result"] != nil {
		t.Fatalf("compaction_end = %+v", ends)
	}
	raw, readErr := os.ReadFile(mark)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(raw), "session_compact_failed") || !strings.Contains(string(raw), compaction.ErrNoLegalCut.Error()) {
		t.Fatalf("mark = %q", raw)
	}
}
