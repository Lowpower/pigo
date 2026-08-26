package sqlite

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

func fixture(t *testing.T) (*Repository, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.sqlite")
	repo, err := NewRepository(Options{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo, dir
}

func create(t *testing.T, repo *Repository, id, cwd string) *sessionrepo.Session {
	t.Helper()
	s, err := repo.Create(sessionrepo.CreateOptions{ID: id, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func userMsg(text string) map[string]any {
	return map[string]any{
		"role":      "user",
		"content":   []map[string]any{{"type": "text", "text": text}},
		"timestamp": 1,
	}
}

func assistantMsg(text string) map[string]any {
	return map[string]any{
		"role":     "assistant",
		"content":  []map[string]any{{"type": "text", "text": text}},
		"api":      "anthropic-messages",
		"provider": "anthropic",
		"model":    "claude-sonnet-4-5",
		"usage": map[string]any{
			"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "totalTokens": 0,
			"cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0},
		},
		"stopReason": "stop",
		"timestamp":  1,
	}
}

func opStarted(id, lane, kind string) sessionrepo.Record {
	intent := map[string]any{"kind": kind}
	switch kind {
	case "run":
		intent["originalPrompt"] = []any{}
		intent["initialMessages"] = []any{}
	case "compaction":
		intent["resultEntryId"] = id + "-result"
	case "navigation":
		intent["targetId"] = nil
		intent["summarize"] = false
	}
	return sessionrepo.Record{Type: "operation_started", ID: id, Lane: lane, Intent: intent}
}

func mustCode(t *testing.T, err error, code sessionrepo.ErrorCode) {
	t.Helper()
	if !sessionrepo.IsCode(err, code) {
		t.Fatalf("got %#v, want code %s", err, code)
	}
}

func ids(ents []sessionrepo.Entry) []string {
	out := make([]string, len(ents))
	for i, e := range ents {
		out[i] = e.ID
	}
	return out
}

func strsEq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestConformanceParentsAndSequence(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	root, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "root", Message: userMsg("hi")}, sessionrepo.MainLane)
	if err != nil {
		t.Fatal(err)
	}
	if root.ParentID != nil || root.Seq != 1 {
		t.Fatalf("root parent/seq: %+v", root)
	}
	if err := s.CreateLane("thread", strPtr(root.ID)); err != nil {
		t.Fatal(err)
	}
	child, err := s.AppendEntry(sessionrepo.Entry{Type: "custom", ID: "child", CustomType: "note", Data: 1, HasData: true}, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentID == nil || *child.ParentID != "root" || child.Seq != 3 {
		t.Fatalf("child: %+v", child)
	}
	rec, err := s.AppendRecord(opStarted("run", "thread", "run"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Seq != 4 {
		t.Fatalf("record seq %d", rec.Seq)
	}
	name := "Example"
	if err := s.SetName(&name); err != nil {
		t.Fatal(err)
	}
	label := "checkpoint"
	if err := s.SetLabel(root.ID, &label); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveLane(sessionrepo.MainLane, strPtr(child.ID)); err != nil {
		t.Fatal(err)
	}
	log, err := s.GetLog(sessionrepo.LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []string{"entry", "lane", "entry", "record", "fact", "fact", "lane"}
	if len(log) != 7 {
		t.Fatalf("log len %d %+v", len(log), log)
	}
	for i, k := range wantKinds {
		if log[i].Kind != k || log[i].Seq != int64(i+1) {
			t.Fatalf("log[%d]=%s seq=%d", i, log[i].Kind, log[i].Seq)
		}
	}
	lanes, err := s.GetLanes()
	if err != nil {
		t.Fatal(err)
	}
	if len(lanes) != 2 || lanes[0].Lane != "main" || lanes[1].Lane != "thread" {
		t.Fatalf("lanes %+v", lanes)
	}
	if lanes[0].LeafID == nil || *lanes[0].LeafID != "child" || lanes[1].LeafID == nil || *lanes[1].LeafID != "child" {
		t.Fatalf("leaves %+v", lanes)
	}
}

func TestConformanceDuplicateIDs(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "shared", Message: userMsg("x")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	_, err := s.AppendRecord(opStarted("shared", sessionrepo.MainLane, "run"))
	mustCode(t, err, sessionrepo.ErrAlreadyExists)
	if _, err := s.AppendRecord(opStarted("run", sessionrepo.MainLane, "run")); err != nil {
		t.Fatal(err)
	}
	_, err = s.AppendEntry(sessionrepo.Entry{Type: "custom", ID: "run", CustomType: "note"}, sessionrepo.MainLane)
	mustCode(t, err, sessionrepo.ErrAlreadyExists)
	log, err := s.GetLog(sessionrepo.LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].Seq != 1 || log[1].Seq != 2 {
		t.Fatalf("log %+v", log)
	}
}

func TestConformanceLaneIsolation(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "root", Message: userMsg("root")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateLane("thread", strPtr("root")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "main-child", Message: userMsg("main")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "thread-child", Message: userMsg("thread")}, "thread"); err != nil {
		t.Fatal(err)
	}
	mainBr, err := s.FindEntriesOnBranch(sessionrepo.EntryQuery{Start: "main-child", Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(mainBr), []string{"root", "main-child"})
	thBr, err := s.FindEntriesOnBranch(sessionrepo.EntryQuery{Start: "thread-child", Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(thBr), []string{"root", "thread-child"})
}

func TestConformanceLaneLifecycle(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	mustCode(t, s.CreateLane(sessionrepo.MainLane, nil), sessionrepo.ErrAlreadyExists)
	mustCode(t, s.CreateLane("thread", strPtr("missing")), sessionrepo.ErrNotFound)
	mustCode(t, s.MoveLane("missing", nil), sessionrepo.ErrInvalidLane)
}

func TestConformanceQueries(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	appendMsg := func(id string, msg any) {
		t.Helper()
		if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: id, Message: msg}, sessionrepo.MainLane); err != nil {
			t.Fatal(err)
		}
	}
	appendMsg("root", userMsg("root"))
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "custom", ID: "old-note", CustomType: "note", Data: 1, HasData: true}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "compaction", ID: "compact", Summary: "summary", RetainedTail: []any{}, TokensBefore: 10, HasTokensBefore: true}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "custom", ID: "new-note", CustomType: "note", Data: 2, HasData: true}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	appendMsg("tail", assistantMsg("tail"))

	all, err := s.FindEntries(sessionrepo.EntryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(all), []string{"tail", "new-note", "compact", "old-note", "root"})
	page, err := s.FindEntries(sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst, Cursor: &sessionrepo.Cursor{AfterSeq: 2}, Limit: 2, HasLimit: true})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(page), []string{"compact", "new-note"})
	notes, err := s.FindEntries(sessionrepo.EntryQuery{CustomType: "note"})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(notes), []string{"new-note", "old-note"})
	br, err := s.FindEntriesOnBranch(sessionrepo.EntryQuery{Start: "tail", CustomType: "note", Limit: 1, HasLimit: true})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(br), []string{"new-note"})
	msgs, err := s.FindEntriesOnBranch(sessionrepo.EntryQuery{Start: "tail", StopAtType: "compaction", Type: "message"})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(msgs), []string{"tail"})
	_, err = s.FindEntries(sessionrepo.EntryQuery{Limit: 0, HasLimit: true})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
	_, err = s.FindEntriesOnBranch(sessionrepo.EntryQuery{Start: "missing"})
	mustCode(t, err, sessionrepo.ErrNotFound)
}

func TestConformanceInvalidQueries(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "invalid-queries", cwd)
	if err := s.CreateLane("thread", nil); err != nil {
		t.Fatal(err)
	}
	thread := s.View("thread")
	_, err := s.FindEntries(sessionrepo.EntryQuery{Limit: 0, HasLimit: true})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
	_, err = thread.FindEntriesOnBranch(sessionrepo.EntryQuery{Cursor: &sessionrepo.Cursor{AfterSeq: -1}})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
	_, err = s.FindRecords(sessionrepo.RecordQuery{OperationKind: "run"})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
	_, err = s.FindOpenOperations(sessionrepo.MainLane, sessionrepo.OpenOpOptions{Limit: 0, HasLimit: true})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
	_, err = s.GetLog(sessionrepo.LogOptions{AfterSeq: -1, HasAfter: true})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
}

func TestConformanceOpenOperation(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	open, err := s.FindOpenOperations(sessionrepo.MainLane, sessionrepo.OpenOpOptions{Limit: 2, HasLimit: true})
	if err != nil || len(open) != 0 {
		t.Fatalf("initial %+v %v", open, err)
	}
	first, err := s.AppendRecord(opStarted("first", sessionrepo.MainLane, "run"))
	if err != nil {
		t.Fatal(err)
	}
	open, err = s.FindOpenOperations(sessionrepo.MainLane, sessionrepo.OpenOpOptions{Limit: 2, HasLimit: true})
	if err != nil || len(open) != 1 || open[0].ID != first.ID {
		t.Fatalf("after first %+v %v", open, err)
	}
	_, err = s.AppendRecord(opStarted("second", sessionrepo.MainLane, "run"))
	mustCode(t, err, sessionrepo.ErrStorage)
	if _, err := s.AppendRecord(sessionrepo.Record{Type: "operation_finished", ID: "fin", Lane: sessionrepo.MainLane, RunID: first.ID, HasRunID: true, Outcome: "completed"}); err != nil {
		t.Fatal(err)
	}
	open, err = s.FindOpenOperations(sessionrepo.MainLane, sessionrepo.OpenOpOptions{})
	if err != nil || len(open) != 0 {
		t.Fatalf("after finish %+v %v", open, err)
	}
}

func TestConformanceStatsAndFacts(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "user", Message: userMsg("hi")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	asst := assistantMsg("ok")
	asst["usage"] = map[string]any{
		"input": 10, "output": 5, "cacheRead": 3, "cacheWrite": 2, "totalTokens": 20,
		"cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 10},
	}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "asst", Message: asst}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	u1 := sessionrepo.Usage{Input: 10, Output: 5, CacheRead: 3, CacheWrite: 2, TotalTokens: 20, Cost: sessionrepo.UsageCost{Total: 10}}
	if _, err := s.AppendRecord(sessionrepo.Record{Type: "usage", ID: "u1", Lane: sessionrepo.MainLane, Usage: &u1, Cause: "assistant", RunID: "r", HasRunID: true, StopReason: "stop"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateLane("thread", strPtr("user")); err != nil {
		t.Fatal(err)
	}
	u2 := sessionrepo.Usage{TotalTokens: 0, Cost: sessionrepo.UsageCost{Total: 0}}
	if _, err := s.AppendRecord(sessionrepo.Record{Type: "usage", ID: "u2", Lane: "thread", Usage: &u2, Cause: "deferred_fetch", RunID: "r", HasRunID: true, StopReason: "deferred"}); err != nil {
		t.Fatal(err)
	}
	u3 := sessionrepo.Usage{Input: -2, TotalTokens: -2, Cost: sessionrepo.UsageCost{Input: -0.5, Total: -0.5}}
	if _, err := s.AppendRecord(sessionrepo.Record{Type: "usage", ID: "u3", Lane: "thread", Usage: &u3, Cause: "adjustment"}); err != nil {
		t.Fatal(err)
	}
	n1, n2 := "First", "Second"
	if err := s.SetName(&n1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetName(&n2); err != nil {
		t.Fatal(err)
	}
	keep := "keep"
	if err := s.SetLabel("user", &keep); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLabel("user", nil); err != nil {
		t.Fatal(err)
	}
	mustCode(t, s.SetLabel("missing", strPtr("checkpoint")), sessionrepo.ErrNotFound)
	got, err := s.GetName()
	if err != nil || got == nil || *got != "Second" {
		t.Fatalf("name %v %v", got, err)
	}
	lab, err := s.GetLabel("user")
	if err != nil || lab != nil {
		t.Fatalf("label %v %v", lab, err)
	}
	st, err := s.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.MessageCount != 2 || st.CachedTokens != 3 || st.UncachedTokens != 10 || st.TotalTokens != 18 || st.CostTotal != 9.5 {
		t.Fatalf("stats %+v", st)
	}
}

func TestConformanceRejectNonJSON(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	_, err := s.AppendCustomEntry("invalid", math.NaN(), true)
	mustCode(t, err, sessionrepo.ErrInvalidPayload)
	leaf, err := s.GetLeafID()
	if err != nil || leaf != nil {
		t.Fatalf("leaf %v %v", leaf, err)
	}
	ents, err := s.FindEntries(sessionrepo.EntryQuery{})
	if err != nil || len(ents) != 0 {
		t.Fatalf("entries %+v %v", ents, err)
	}
	if _, err := s.AppendCustomEntry("ok", 1, true); err != nil {
		t.Fatal(err)
	}
}

func TestConformanceRepoCRUDAndFork(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "one", cwd)
	eid, err := s.AppendMessage(userMsg("hi"))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := repo.List(sessionrepo.ListOptions{})
	if err != nil || len(listed) != 1 || listed[0].ID != "one" {
		t.Fatalf("list %+v %v", listed, err)
	}
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	opened, err := repo.Open(meta)
	if err != nil {
		t.Fatal(err)
	}
	found, err := opened.FindEntries(sessionrepo.EntryQuery{})
	if err != nil || len(found) != 1 || found[0].ID != eid {
		t.Fatalf("open entries %+v %v", found, err)
	}
	_, err = repo.Create(sessionrepo.CreateOptions{ID: "one", CWD: cwd})
	mustCode(t, err, sessionrepo.ErrAlreadyExists)

	src := create(t, repo, "source", cwd)
	if _, err := src.AppendEntry(sessionrepo.Entry{Type: "message", ID: "root", Message: userMsg("root")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if _, err := src.AppendEntry(sessionrepo.Entry{Type: "message", ID: "shared", Message: assistantMsg("shared")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if err := src.CreateLane("thread", strPtr("shared")); err != nil {
		t.Fatal(err)
	}
	if _, err := src.AppendEntry(sessionrepo.Entry{Type: "message", ID: "threadChild", Message: userMsg("t")}, "thread"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.AppendEntry(sessionrepo.Entry{Type: "message", ID: "mainChild", Message: userMsg("m")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	nm := "Source"
	if err := src.SetName(&nm); err != nil {
		t.Fatal(err)
	}
	copied := "copied"
	if err := src.SetLabel("shared", &copied); err != nil {
		t.Fatal(err)
	}
	excl := "excluded"
	if err := src.SetLabel("threadChild", &excl); err != nil {
		t.Fatal(err)
	}
	if _, err := src.AppendRecord(opStarted("run", sessionrepo.MainLane, "run")); err != nil {
		t.Fatal(err)
	}
	srcMeta, err := src.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	forked, err := repo.Fork(srcMeta, sessionrepo.ForkOptions{
		Scope: "branch", EntryID: "mainChild", HasEntry: true, Position: "at",
		CreateOptions: sessionrepo.CreateOptions{ID: "branch-fork", CWD: cwd},
	})
	if err != nil {
		t.Fatal(err)
	}
	fents, err := forked.FindEntries(sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(fents), []string{"root", "shared", "mainChild"})
	fname, err := forked.GetName()
	if err != nil || fname == nil || *fname != "Source" {
		t.Fatalf("fork name %v %v", fname, err)
	}
	flab, err := forked.GetLabel("shared")
	if err != nil || flab == nil || *flab != "copied" {
		t.Fatalf("fork label %v %v", flab, err)
	}
	missing, err := forked.GetLabel("threadChild")
	if err != nil || missing != nil {
		t.Fatalf("excluded label %v %v", missing, err)
	}
	recs, err := forked.FindRecords(sessionrepo.RecordQuery{})
	if err != nil || len(recs) != 0 {
		t.Fatalf("fork records %+v %v", recs, err)
	}
	st, err := forked.GetStats()
	if err != nil || st.MessageCount != 3 {
		t.Fatalf("fork stats %+v %v", st, err)
	}
	fm, err := forked.GetMetadata()
	if err != nil || fm.ParentSessionID != "source" {
		t.Fatalf("fork meta %+v %v", fm, err)
	}

	if err := repo.Delete(meta); err != nil {
		t.Fatal(err)
	}
	_, err = repo.Open(meta)
	mustCode(t, err, sessionrepo.ErrNotFound)
	if err := repo.Delete(meta); err != nil {
		t.Fatal(err)
	}
}

func TestConformanceForkBefore(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "src", cwd)
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "root", Message: userMsg("r")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "tail", Message: userMsg("t")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	forked, err := repo.Fork(meta, sessionrepo.ForkOptions{
		HasEntry: true, EntryID: "tail",
		CreateOptions: sessionrepo.CreateOptions{ID: "fork", CWD: cwd},
	})
	if err != nil {
		t.Fatal(err)
	}
	ents, err := forked.FindEntries(sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(ents), []string{"root"})
	leaf, err := forked.GetLeafID()
	if err != nil || leaf == nil || *leaf != "root" {
		t.Fatalf("fork leaf %v %v", leaf, err)
	}
	srcLeaf, err := s.GetLeafID()
	if err != nil || srcLeaf == nil || *srcLeaf != "tail" {
		t.Fatalf("src leaf %v %v", srcLeaf, err)
	}
	_, err = repo.Fork(meta, sessionrepo.ForkOptions{HasEntry: true, EntryID: "missing", CreateOptions: sessionrepo.CreateOptions{CWD: cwd}})
	mustCode(t, err, sessionrepo.ErrInvalidForkTarget)

	onlyCustom := create(t, repo, "custom-src", cwd)
	if _, err := onlyCustom.AppendEntry(sessionrepo.Entry{Type: "custom", ID: "c", CustomType: "note"}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	cm, err := onlyCustom.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Fork(cm, sessionrepo.ForkOptions{CreateOptions: sessionrepo.CreateOptions{ID: "fork2", CWD: cwd}})
	mustCode(t, err, sessionrepo.ErrInvalidForkTarget)
}

func TestConformanceClearName(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	tmp := "Temporary"
	if err := s.SetName(&tmp); err != nil {
		t.Fatal(err)
	}
	if err := s.SetName(nil); err != nil {
		t.Fatal(err)
	}
	n, err := s.GetName()
	if err != nil || n != nil {
		t.Fatalf("name %v %v", n, err)
	}
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	opened, err := repo.Open(meta)
	if err != nil {
		t.Fatal(err)
	}
	n, err = opened.GetName()
	if err != nil || n != nil {
		t.Fatalf("reopen name %v %v", n, err)
	}
}

func TestMigrationsSchema(t *testing.T) {
	repo, _ := fixture(t)
	s := create(t, repo, "x", t.TempDir())
	_ = s
	db := repo.db
	var id string
	if err := db.QueryRow(`SELECT id FROM migrations`).Scan(&id); err != nil || id != "001_initial.sql" {
		t.Fatalf("migration %q %v", id, err)
	}
	tables := []string{"sessions", "entries", "session_sequences", "session_stats", "branch_entries", "branch_tips", "lanes", "records", "lane_moves", "facts", "writer_leases"}
	for _, name := range tables {
		ok, err := tableExists(db, name)
		if err != nil || !ok {
			t.Fatalf("missing table %s: %v", name, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'leaf_id'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("sessions.leaf_id should not exist")
	}
}

func TestWriterLeaseSecondWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.sqlite")
	r1, err := NewRepository(Options{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r1.Close() }()
	s := create(t, r1, "sess", dir)
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	r2, err := NewRepository(Options{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r2.Close() }()
	_, err = r2.Open(meta)
	mustCode(t, err, sessionrepo.ErrStorage)
	_ = r1.Close()
	opened, err := r2.Open(meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.AppendMessage(userMsg("after")); err != nil {
		t.Fatal(err)
	}
}

func TestSearchLazyFTS(t *testing.T) {
	repo, cwd := fixture(t)
	path := repo.absPath
	search, err := NewSearch(path)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := search.Search(t.Context(), "   ", sessionrepo.SearchOptions{})
	if err != nil || len(hits) != 0 {
		t.Fatalf("blank search %v %v", hits, err)
	}
	s := create(t, repo, "a", cwd)
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "m", Message: userMsg("Find the auth defect")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	ok, err := tableExists(repo.db, "session_search_fts")
	if err != nil || ok {
		t.Fatalf("fts should not exist yet: %v %v", ok, err)
	}
	hits, err = search.Search(t.Context(), "auth", sessionrepo.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].EntryID != "m" {
		t.Fatalf("hits %+v", hits)
	}
}

func TestJSONLDefaultUntouched(t *testing.T) {
	root := testdataRoot(t)
	if _, err := os.Stat(filepath.Join(root, "internal", "session", "session.go")); err != nil {
		t.Fatal(err)
	}
}

func testdataRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
