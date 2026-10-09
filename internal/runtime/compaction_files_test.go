package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/session"
)

func TestFirstCompactionTracksFiles(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "start"}); err != nil {
		t.Fatal(err)
	}
	appendToolAssistant(t, sess,
		toolUse("read", "a.go"),
		toolUse("edit", "b.go"),
		toolUse("write", "c.go"),
		toolUse("grep", "d.go"),
	)
	tail := appendUserText(t, sess, strings.Repeat("z", 80))
	var prompt string
	var convo []ai.Message
	e := compactionEngine(sess, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		prompt = req.Messages[len(req.Messages)-1].Content
		convo = append([]ai.Message(nil), req.Messages[:len(req.Messages)-1]...)
		return textReply("FIRST")(ctx, req, opts)
	})
	msgs := session.ModelMessages(session.ContextEntries(sess))
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 20}, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "<previous-summary>") || strings.Contains(prompt, "PRESERVE all existing information") {
		t.Fatalf("first compaction used the update prompt:\n%s", prompt)
	}
	if strings.Contains(conversationText(convo), "<summary>") {
		t.Fatalf("summarized the previous summary message: %+v", convo)
	}
	entry := lastCompaction(t, sess)
	if entry.FromHook || entry.FirstKeptEntryID != tail.ID {
		t.Fatalf("entry fromHook=%v firstKept=%s want %s", entry.FromHook, entry.FirstKeptEntryID, tail.ID)
	}
	lists, ok := compaction.ParseStoredFiles(entry.Details)
	if !ok {
		t.Fatal("missing details")
	}
	if strings.Join(lists.ReadFiles, ",") != "a.go" || strings.Join(lists.ModifiedFiles, ",") != "b.go,c.go" {
		t.Fatalf("files = %+v", lists)
	}
	if !strings.Contains(entry.Summary, "FIRST") || !strings.Contains(entry.Summary, "<read-files>\na.go\n</read-files>") {
		t.Fatalf("summary = %q", entry.Summary)
	}
	if strings.Contains(entry.Summary, "d.go") {
		t.Fatalf("grep path was tracked: %q", entry.Summary)
	}
}

func TestSecondCompactionUpdatesSummaryAndAccumulatesFiles(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "start"}); err != nil {
		t.Fatal(err)
	}
	appendToolAssistant(t, sess, toolUse("read", "a.go"), toolUse("edit", "b.go"))
	kept := appendUserText(t, sess, "KEPT-TAIL-"+strings.Repeat("k", 80))
	e := compactionEngine(sess, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		return textReply("FIRST")(ctx, req, opts)
	})
	msgs := session.ModelMessages(session.ContextEntries(sess))
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 20}, false); err != nil {
		t.Fatal(err)
	}
	if lastCompaction(t, sess).FirstKeptEntryID != kept.ID {
		t.Fatalf("first kept = %s", lastCompaction(t, sess).FirstKeptEntryID)
	}
	appendToolAssistant(t, sess, toolUse("read", "d.go"), toolUse("edit", "e.go"))
	appendUserText(t, sess, strings.Repeat("y", 80))

	var prompt string
	var convo []ai.Message
	e.Stream = func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		prompt = req.Messages[len(req.Messages)-1].Content
		convo = append([]ai.Message(nil), req.Messages[:len(req.Messages)-1]...)
		return textReply("SECOND")(ctx, req, opts)
	}
	msgs = session.ModelMessages(session.ContextEntries(sess))
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 20}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "<previous-summary>\n") || !strings.Contains(prompt, "FIRST") {
		t.Fatalf("prompt missing previous summary:\n%s", prompt)
	}
	if !strings.Contains(prompt, "PRESERVE all existing information from the previous summary") {
		t.Fatalf("prompt missing update instructions:\n%s", prompt)
	}
	text := conversationText(convo)
	if !strings.Contains(text, "KEPT-TAIL-") {
		t.Fatalf("summarized messages dropped the kept tail: %s", text)
	}
	if strings.Contains(text, compaction.SummaryPrefix) {
		t.Fatalf("summarized the compaction entry itself: %s", text)
	}
	lists, ok := compaction.ParseStoredFiles(lastCompaction(t, sess).Details)
	if !ok {
		t.Fatal("missing details")
	}
	if strings.Join(lists.ReadFiles, ",") != "a.go,d.go" {
		t.Fatalf("readFiles = %v", lists.ReadFiles)
	}
	if strings.Join(lists.ModifiedFiles, ",") != "b.go,e.go" {
		t.Fatalf("modifiedFiles = %v", lists.ModifiedFiles)
	}
}

func TestFromHookCompactionDoesNotAccumulateFiles(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "BEFORE-UNIQUE"}); err != nil {
		t.Fatal(err)
	}
	kept := appendUserText(t, sess, "KEPT-"+strings.Repeat("k", 80))
	if _, err := sess.AppendCompaction("HOOK-SUM", kept.ID, 3, session.CompactionMeta{
		FromHook: true,
		Details:  map[string]any{"readFiles": []string{"secret.go"}, "modifiedFiles": []string{"hidden.go"}},
	}); err != nil {
		t.Fatal(err)
	}
	appendToolAssistant(t, sess, toolUse("read", "other.go"))
	appendUserText(t, sess, strings.Repeat("t", 80))
	var prompt string
	var convo []ai.Message
	e := compactionEngine(sess, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		prompt = req.Messages[len(req.Messages)-1].Content
		convo = append([]ai.Message(nil), req.Messages[:len(req.Messages)-1]...)
		return textReply("NEXT")(ctx, req, opts)
	})
	msgs := session.ModelMessages(session.ContextEntries(sess))
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 20}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "<previous-summary>\nHOOK-SUM\n</previous-summary>") {
		t.Fatalf("prompt = %s", prompt)
	}
	text := conversationText(convo)
	if strings.Contains(text, "BEFORE-UNIQUE") || strings.Contains(text, compaction.SummaryPrefix) {
		t.Fatalf("summarized outside the kept boundary: %s", text)
	}
	if !strings.Contains(text, "KEPT-") {
		t.Fatalf("dropped kept tail: %s", text)
	}
	lists, ok := compaction.ParseStoredFiles(lastCompaction(t, sess).Details)
	if !ok {
		t.Fatal("missing details")
	}
	if strings.Join(lists.ReadFiles, ",") != "other.go" || len(lists.ModifiedFiles) != 0 {
		t.Fatalf("files = %+v", lists)
	}
}

func TestMissingFirstKeptStartsAfterCompaction(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "BEFORE-UNIQUE"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendCompaction("PREV-SUM", "missing-id", 1, session.CompactionMeta{
		Details: map[string]any{"readFiles": []string{"old.go"}},
	}); err != nil {
		t.Fatal(err)
	}
	appendUserText(t, sess, "AFTER-UNIQUE-"+strings.Repeat("a", 40))
	appendUserText(t, sess, strings.Repeat("t", 80))
	var convo []ai.Message
	e := compactionEngine(sess, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		convo = append([]ai.Message(nil), req.Messages[:len(req.Messages)-1]...)
		return textReply("NEXT")(ctx, req, opts)
	})
	msgs := session.ModelMessages(session.ContextEntries(sess))
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 20}, false); err != nil {
		t.Fatal(err)
	}
	text := conversationText(convo)
	if strings.Contains(text, "BEFORE-UNIQUE") {
		t.Fatalf("summarized entries before the missing boundary: %s", text)
	}
	if !strings.Contains(text, "AFTER-UNIQUE-") {
		t.Fatalf("dropped messages after the compaction: %s", text)
	}
	lists, ok := compaction.ParseStoredFiles(lastCompaction(t, sess).Details)
	if !ok || strings.Join(lists.ReadFiles, ",") != "old.go" {
		t.Fatalf("files = %+v ok=%v", lists, ok)
	}
}

func TestCompactionWritesEmptyFileLists(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	appendUserText(t, sess, "older")
	appendUserText(t, sess, strings.Repeat("z", 80))
	e := compactionEngine(sess, textReply("SUM"))
	msgs := session.ModelMessages(session.ContextEntries(sess))
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 20}, false); err != nil {
		t.Fatal(err)
	}
	raw := string(lastCompaction(t, sess).Details)
	if !strings.Contains(raw, `"readFiles":[]`) || !strings.Contains(raw, `"modifiedFiles":[]`) {
		t.Fatalf("details = %s", raw)
	}
}

func TestExtensionCompactionMarksFromHook(t *testing.T) {
	h := spawnRuntimeExt(t, "compact", nil)
	sess := session.New(t.TempDir(), t.TempDir())
	appendUserText(t, sess, "hi")
	e := compactionEngine(sess, textReply("unused"))
	e.Hosts = []*ext.Host{h}
	msgs := session.ModelMessages(session.ContextEntries(sess))
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 1}, false); err != nil {
		t.Fatal(err)
	}
	entry := lastCompaction(t, sess)
	if !entry.FromHook || entry.Summary != "HOOK SUMMARY" || len(entry.Details) != 0 {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestBranchSummaryWritesAccumulatedFiles(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "base"}); err != nil {
		t.Fatal(err)
	}
	stay, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "stay"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "side"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendBranchSummary("old", "hooked", true, map[string]any{"readFiles": []string{"skip.go"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendBranchSummary("old", "nested", false, map[string]any{
		"readFiles": []string{"h.go"}, "modifiedFiles": []string{"m.go"},
	}); err != nil {
		t.Fatal(err)
	}
	appendToolAssistant(t, sess, toolUse("read", "f.go"), toolUse("edit", "g.go"))
	appendUserText(t, sess, "leaf")
	e := compactionEngine(sess, textReply("BRANCH"))
	if _, err := e.NavigateTree(context.Background(), stay.ID, session.NavigateOpts{Summarize: true}); err != nil {
		t.Fatal(err)
	}
	entry := lastBranchSummary(t, sess)
	if entry.FromHook {
		t.Fatal("generated branch summary marked fromHook")
	}
	lists, ok := compaction.ParseStoredFiles(entry.Details)
	if !ok {
		t.Fatal("missing details")
	}
	if strings.Join(lists.ReadFiles, ",") != "f.go,h.go" {
		t.Fatalf("readFiles = %v", lists.ReadFiles)
	}
	if strings.Join(lists.ModifiedFiles, ",") != "g.go,m.go" {
		t.Fatalf("modifiedFiles = %v", lists.ModifiedFiles)
	}
	if !strings.Contains(entry.Summary, "BRANCH") || !strings.Contains(entry.Summary, "<read-files>\nf.go\nh.go\n</read-files>") {
		t.Fatalf("summary = %q", entry.Summary)
	}
	if strings.Contains(entry.Summary, "skip.go") {
		t.Fatalf("fromHook file was accumulated: %q", entry.Summary)
	}
}

func TestHookBranchSummaryOmitsFileDetails(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "base"}); err != nil {
		t.Fatal(err)
	}
	stay, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "stay"})
	if err != nil {
		t.Fatal(err)
	}
	appendUserText(t, sess, "leaf")
	e := compactionEngine(sess, textReply("unused"))
	e.BeforeTree = func(session.TreePrep) session.TreeHookResult {
		return session.TreeHookResult{Summary: "ext branch"}
	}
	if _, err := e.NavigateTree(context.Background(), stay.ID, session.NavigateOpts{Summarize: true}); err != nil {
		t.Fatal(err)
	}
	entry := lastBranchSummary(t, sess)
	if !entry.FromHook || entry.Summary != "ext branch" || len(entry.Details) != 0 {
		t.Fatalf("entry = %+v", entry)
	}
}

func compactionEngine(sess *session.Manager, sf ai.StreamFn) *Engine {
	return &Engine{
		Opts:   Options{Session: sess, Config: config.Config{Model: "x"}},
		Stream: sf,
	}
}

func appendUserText(t *testing.T, m *session.Manager, text string) *session.Entry {
	t.Helper()
	e, err := m.AppendMessage("user", map[string]any{"role": "user", "content": text})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func appendToolAssistant(t *testing.T, m *session.Manager, calls ...map[string]any) {
	t.Helper()
	content := []map[string]any{{"type": "text", "text": "tools"}}
	content = append(content, calls...)
	if _, err := m.AppendMessage("assistant", map[string]any{
		"role": "assistant", "stopReason": "toolUse", "content": content,
	}); err != nil {
		t.Fatal(err)
	}
}

func toolUse(name, path string) map[string]any {
	return map[string]any{
		"type": "toolCall", "id": name + "-" + path, "name": name,
		"arguments": map[string]any{"path": path},
	}
}

func lastCompaction(t *testing.T, m *session.Manager) session.Entry {
	t.Helper()
	var found session.Entry
	ok := false
	for _, e := range m.GetBranch("") {
		if e.Type == "compaction" {
			found = e
			ok = true
		}
	}
	if !ok {
		t.Fatal("no compaction entry")
	}
	return found
}

func lastBranchSummary(t *testing.T, m *session.Manager) session.Entry {
	t.Helper()
	leaf := m.LeafID()
	for _, e := range m.Entries() {
		if e.ID == leaf && e.Type == "branch_summary" {
			return e
		}
	}
	t.Fatal("leaf is not a branch summary")
	return session.Entry{}
}

func conversationText(msgs []ai.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}
