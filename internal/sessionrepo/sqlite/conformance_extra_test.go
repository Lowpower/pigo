package sqlite

import (
	"math"
	"sync"
	"testing"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

func TestConformanceRecordsAndLaneMoves(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	root, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "root", Message: userMsg("root")}, sessionrepo.MainLane)
	if err != nil {
		t.Fatal(err)
	}
	finished, err := s.AppendRecord(sessionrepo.Record{
		Type: "operation_finished", ID: "finish", Lane: sessionrepo.MainLane, RunID: "run", HasRunID: true, Outcome: "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Seq != 2 {
		t.Fatalf("seq %d", finished.Seq)
	}
	lanes, err := s.GetLanes()
	if err != nil || len(lanes) != 1 || lanes[0].LeafID == nil || *lanes[0].LeafID != "root" {
		t.Fatalf("lanes %+v %v", lanes, err)
	}
	if err := s.MoveLane(sessionrepo.MainLane, nil); err != nil {
		t.Fatal(err)
	}
	lanes, err = s.GetLanes()
	if err != nil || lanes[0].LeafID != nil {
		t.Fatalf("cleared leaf %+v %v", lanes, err)
	}
	log, err := s.GetLog(sessionrepo.LogOptions{})
	if err != nil || len(log) != 3 {
		t.Fatalf("log %+v %v", log, err)
	}
	if log[0].Kind != "entry" || log[0].Entry == nil || log[0].Entry.ID != root.ID {
		t.Fatalf("log[0] %+v", log[0])
	}
	if log[1].Kind != "record" || log[1].Record == nil || log[1].Record.ID != finished.ID {
		t.Fatalf("log[1] %+v", log[1])
	}
	if log[2].Kind != "lane" || log[2].Lane != sessionrepo.MainLane || log[2].LeafID != nil {
		t.Fatalf("log[2] %+v", log[2])
	}
	mustCode(t, s.MoveLane(sessionrepo.MainLane, strPtr("missing")), sessionrepo.ErrNotFound)
	recs, err := s.FindRecords(sessionrepo.RecordQuery{})
	if err != nil || len(recs) != 1 {
		t.Fatalf("records %+v %v", recs, err)
	}
}

func TestConformanceQueueCancellation(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	enqueued, err := s.AppendRecord(sessionrepo.Record{
		Type: "queue_enqueued", ID: "enqueue", Lane: sessionrepo.MainLane, Queue: "nextRun",
		Target: map[string]any{"type": "message", "id": "queued-message", "message": userMsg("queued")},
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.AppendRecord(sessionrepo.Record{
		Type: "queue_cancelled", ID: "cancel", Lane: sessionrepo.MainLane, EntryID: "queued-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Seq != 2 || cancelled.EntryID != "queued-message" || cancelled.HasRunID || cancelled.RunID != "" {
		t.Fatalf("cancelled %+v", cancelled)
	}
	got, err := s.GetEntry("queued-message")
	if err != nil || got != nil {
		t.Fatalf("target should not be stored: %v %v", got, err)
	}
	found, err := s.FindRecords(sessionrepo.RecordQuery{Type: "queue_cancelled"})
	if err != nil || len(found) != 1 || found[0].EntryID != "queued-message" {
		t.Fatalf("cancellations %+v %v", found, err)
	}
	log, err := s.GetLog(sessionrepo.LogOptions{})
	if err != nil || len(log) != 2 || log[0].Record == nil || log[0].Record.ID != enqueued.ID {
		t.Fatalf("log %+v %v", log, err)
	}
}

func TestConformanceRecordFilters(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	if _, err := s.AppendRecord(opStarted("run-1", sessionrepo.MainLane, "run")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendRecord(sessionrepo.Record{
		Type: "step_attempt", ID: "attempt-1", Lane: sessionrepo.MainLane, RunID: "run-1", HasRunID: true,
		Step: "assistant", Attempt: 1, ResultEntryID: "assistant-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateLane("thread", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendRecord(opStarted("run-2", "thread", "run")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendRecord(sessionrepo.Record{
		Type: "step_attempt", ID: "attempt-2", Lane: "thread", RunID: "run-2", HasRunID: true,
		Step: "assistant", Attempt: 1, ResultEntryID: "assistant-2",
	}); err != nil {
		t.Fatal(err)
	}
	thread, err := s.FindRecords(sessionrepo.RecordQuery{Lane: "thread"})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, recordIDs(thread), []string{"attempt-2", "run-2"})
	attempts, err := s.FindRecords(sessionrepo.RecordQuery{Type: "step_attempt", Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, recordIDs(attempts), []string{"attempt-1", "attempt-2"})
	run1, err := s.FindRecords(sessionrepo.RecordQuery{RunID: "run-1", HasRunID: true, AfterSeq: 1, HasAfterSeq: true})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, recordIDs(run1), []string{"attempt-1"})
	limited, err := s.FindRecords(sessionrepo.RecordQuery{Limit: 1, HasLimit: true})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, recordIDs(limited), []string{"attempt-2"})
}

func TestConformanceOperationKindFilter(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	startFinish := func(id, kind string) {
		t.Helper()
		if _, err := s.AppendRecord(opStarted(id, sessionrepo.MainLane, kind)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AppendRecord(sessionrepo.Record{
			Type: "operation_finished", ID: id + "-finished", Lane: sessionrepo.MainLane, RunID: id, HasRunID: true, Outcome: "completed",
		}); err != nil {
			t.Fatal(err)
		}
	}
	startFinish("run-old", "run")
	startFinish("compaction", "compaction")
	startFinish("navigation", "navigation")
	if _, err := s.AppendRecord(opStarted("run-new", sessionrepo.MainLane, "run")); err != nil {
		t.Fatal(err)
	}
	runs, err := s.FindRecords(sessionrepo.RecordQuery{Type: "operation_started", OperationKind: "run", Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, recordIDs(runs), []string{"run-old", "run-new"})
	comp, err := s.FindRecords(sessionrepo.RecordQuery{Type: "operation_started", OperationKind: "compaction"})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, recordIDs(comp), []string{"compaction"})
	nav, err := s.FindRecords(sessionrepo.RecordQuery{Type: "operation_started", OperationKind: "navigation"})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, recordIDs(nav), []string{"navigation"})
	latest, err := s.FindRecords(sessionrepo.RecordQuery{Type: "operation_started", OperationKind: "run", Limit: 1, HasLimit: true})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, recordIDs(latest), []string{"run-new"})
}

func TestConformanceOrphanFinishDoesNotCloseLaterStart(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	if _, err := s.AppendRecord(sessionrepo.Record{
		Type: "operation_finished", ID: "finish-before-start", Lane: sessionrepo.MainLane, RunID: "run", HasRunID: true, Outcome: "completed",
	}); err != nil {
		t.Fatal(err)
	}
	started, err := s.AppendRecord(opStarted("run", sessionrepo.MainLane, "run"))
	if err != nil {
		t.Fatal(err)
	}
	open, err := s.FindOpenOperations(sessionrepo.MainLane, sessionrepo.OpenOpOptions{Limit: 2, HasLimit: true})
	if err != nil || len(open) != 1 || open[0].ID != started.ID {
		t.Fatalf("open %+v %v", open, err)
	}
}

func TestConformanceOpenOperationsByLane(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	if err := s.CreateLane("thread", nil); err != nil {
		t.Fatal(err)
	}
	mainRun, err := s.AppendRecord(opStarted("main-run", sessionrepo.MainLane, "run"))
	if err != nil {
		t.Fatal(err)
	}
	threadNav, err := s.AppendRecord(opStarted("thread-navigation", "thread", "navigation"))
	if err != nil {
		t.Fatal(err)
	}
	main, err := s.FindOpenOperations(sessionrepo.MainLane, sessionrepo.OpenOpOptions{})
	if err != nil || len(main) != 1 || main[0].ID != mainRun.ID {
		t.Fatalf("main %+v %v", main, err)
	}
	limited, err := s.FindOpenOperations(sessionrepo.MainLane, sessionrepo.OpenOpOptions{Limit: 1, HasLimit: true})
	if err != nil || len(limited) != 1 || limited[0].ID != mainRun.ID {
		t.Fatalf("limited %+v %v", limited, err)
	}
	thread, err := s.FindOpenOperations("thread", sessionrepo.OpenOpOptions{Limit: 2, HasLimit: true})
	if err != nil || len(thread) != 1 || thread[0].ID != threadNav.ID {
		t.Fatalf("thread %+v %v", thread, err)
	}
}

func TestConformanceImmutableOpenOperations(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	committed, err := s.AppendRecord(opStarted("run", sessionrepo.MainLane, "run"))
	if err != nil {
		t.Fatal(err)
	}
	open, err := s.FindOpenOperations(sessionrepo.MainLane, sessionrepo.OpenOpOptions{})
	if err != nil || len(open) != 1 {
		t.Fatalf("open %+v %v", open, err)
	}
	open[0].Intent["originalPrompt"] = append(asSlice(open[0].Intent["originalPrompt"]), userMsg("mutated"))
	again, err := s.FindOpenOperations(sessionrepo.MainLane, sessionrepo.OpenOpOptions{})
	if err != nil || len(again) != 1 {
		t.Fatal(err)
	}
	if again[0].ID != committed.ID {
		t.Fatalf("id %s", again[0].ID)
	}
	prompt, _ := again[0].Intent["originalPrompt"].([]any)
	if len(prompt) != 0 {
		t.Fatalf("mutated stored intent: %+v", again[0].Intent)
	}
}

func TestConformanceQueryStopBounds(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "root", Message: userMsg("root")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "custom", ID: "old-note", CustomType: "note", Data: 1, HasData: true}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "compaction", ID: "compact", Summary: "summary", RetainedTail: []any{}, TokensBefore: 10, HasTokensBefore: true}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "custom", ID: "new-note", CustomType: "note", Data: 2, HasData: true}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "tail", Message: assistantMsg("tail")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	empty, err := s.FindEntriesOnBranch(sessionrepo.EntryQuery{Start: "tail", StopAtID: "tail", Type: "custom"})
	if err != nil || len(empty) != 0 {
		t.Fatalf("stop at id %+v %v", empty, err)
	}
	oldest, err := s.FindEntriesOnBranch(sessionrepo.EntryQuery{Start: "tail", StopAtType: "custom", Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(oldest), []string{"root", "old-note"})
}

func TestConformanceInvalidQueriesExtra(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "invalid-queries", cwd)
	if err := s.CreateLane("thread", nil); err != nil {
		t.Fatal(err)
	}
	thread := s.View("thread")
	_, err := s.FindEntry(sessionrepo.EntryQuery{Limit: 0, HasLimit: true})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
	_, err = s.FindEntriesOnBranch(sessionrepo.EntryQuery{Limit: 0, HasLimit: true})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
	_, err = thread.FindEntryOnBranch(sessionrepo.EntryQuery{Limit: 0, HasLimit: true})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
	_, err = s.FindRecords(sessionrepo.RecordQuery{Limit: 0, HasLimit: true})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
	_, err = s.FindRecords(sessionrepo.RecordQuery{Type: "step_attempt", OperationKind: "run"})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
	_, err = s.FindOpenOperations(sessionrepo.MainLane, sessionrepo.OpenOpOptions{Limit: -1, HasLimit: true})
	mustCode(t, err, sessionrepo.ErrInvalidQuery)
}

func TestConformanceClearNameLogAndFork(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	tmp := "Temporary"
	if err := s.SetName(&tmp); err != nil {
		t.Fatal(err)
	}
	if err := s.SetName(nil); err != nil {
		t.Fatal(err)
	}
	log, err := s.GetLog(sessionrepo.LogOptions{})
	if err != nil || len(log) != 2 || log[0].Name == nil || *log[0].Name != "Temporary" || log[1].Name != nil {
		t.Fatalf("log %+v %v", log, err)
	}
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if meta.HasName || meta.Name != "" {
		t.Fatalf("metadata still has name %+v", meta)
	}
	fork, err := repo.Fork(meta, sessionrepo.ForkOptions{CreateOptions: sessionrepo.CreateOptions{ID: "fork", CWD: cwd}})
	if err != nil {
		t.Fatal(err)
	}
	n, err := fork.GetName()
	if err != nil || n != nil {
		t.Fatalf("fork name %v %v", n, err)
	}
}

func TestConformanceImmutableCopies(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "immutable", cwd)
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"nested": map[string]any{"value": 1}}
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "custom", ID: "custom", CustomType: "note", Data: data, HasData: true}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	data["nested"].(map[string]any)["value"] = 50
	read, err := s.GetEntry("custom")
	if err != nil || read == nil {
		t.Fatal(err)
	}
	read.Data.(map[string]any)["nested"].(map[string]any)["value"] = 99
	readMeta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	readMeta.ID = "changed"
	log, err := s.GetLog(sessionrepo.LogOptions{})
	if err != nil || len(log) == 0 || log[0].Entry == nil {
		t.Fatal(err)
	}
	log[0].Entry.Data.(map[string]any)["nested"].(map[string]any)["value"] = 100
	again, err := s.GetMetadata()
	if err != nil || again.ID != meta.ID {
		t.Fatalf("metadata mutated %+v", again)
	}
	stored, err := s.GetEntry("custom")
	if err != nil || stored == nil {
		t.Fatal(err)
	}
	got := stored.Data.(map[string]any)["nested"].(map[string]any)["value"]
	if got != float64(1) && got != 1 {
		t.Fatalf("stored data %v", stored.Data)
	}
}

func TestConformanceLaneViewsDoNotCacheLeaves(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	root, err := s.AppendMessage(userMsg("root"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateLane("thread", strPtr(root)); err != nil {
		t.Fatal(err)
	}
	thread := s.View("thread")
	var mainChild, threadChild string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		id, err := s.AppendMessage(userMsg("main"))
		if err != nil {
			t.Error(err)
			return
		}
		mainChild = id
	}()
	go func() {
		defer wg.Done()
		id, err := thread.AppendMessage(userMsg("thread"))
		if err != nil {
			t.Error(err)
			return
		}
		threadChild = id
	}()
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}
	leaf, err := s.GetLeafID()
	if err != nil || leaf == nil || *leaf != mainChild {
		t.Fatalf("main leaf %v want %s", leaf, mainChild)
	}
	tLeaf, err := thread.GetLeafID()
	if err != nil || tLeaf == nil || *tLeaf != threadChild {
		t.Fatalf("thread leaf %v want %s", tLeaf, threadChild)
	}
	mainBr, err := s.FindEntriesOnBranch(sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(mainBr), []string{root, mainChild})
	thBr, err := thread.FindEntriesOnBranch(sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(thBr), []string{root, threadChild})
	empty := create(t, repo, "empty", cwd)
	none, err := empty.FindEntriesOnBranch(sessionrepo.EntryQuery{})
	if err != nil || len(none) != 0 {
		t.Fatalf("empty branch %+v %v", none, err)
	}
}

func TestConformanceProvisionedIDsAndTerminate(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	entry, err := s.AppendEntry(sessionrepo.Entry{Type: "custom", ID: "provisioned", CustomType: "note", Data: map[string]any{"value": 1}, HasData: true}, sessionrepo.MainLane)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != "provisioned" || entry.ParentID != nil || entry.Seq != 1 {
		t.Fatalf("entry %+v", entry)
	}
	term := true
	tool, err := s.AppendEntry(sessionrepo.Entry{
		Type: "message", ID: "tool-result", Terminate: &term,
		Message: map[string]any{
			"role": "toolResult", "toolCallId": "call-1", "toolName": "example",
			"content": []map[string]any{{"type": "text", "text": "done"}},
			"isError": false, "timestamp": 1,
		},
	}, sessionrepo.MainLane)
	if err != nil {
		t.Fatal(err)
	}
	if tool.Terminate == nil || !*tool.Terminate {
		t.Fatalf("terminate %+v", tool)
	}
	stored, err := s.GetEntry(tool.ID)
	if err != nil || stored == nil || stored.Terminate == nil || !*stored.Terminate {
		t.Fatalf("stored %+v %v", stored, err)
	}
}

func TestConformanceRejectNonJSONRecords(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	_, err := s.AppendRecord(sessionrepo.Record{
		Type: "tool_started", ID: "nan-record", Lane: sessionrepo.MainLane, RunID: "run", HasRunID: true,
		AssistantEntryID: "assistant", ToolCallID: "call", ToolName: "example",
		EffectiveArgs: map[string]any{"value": math.NaN()}, ResultEntryID: "result", Replay: "never",
	})
	mustCode(t, err, sessionrepo.ErrInvalidPayload)
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	_, err = s.AppendCustomEntry("invalid", cyclic, true)
	mustCode(t, err, sessionrepo.ErrInvalidPayload)
	recs, err := s.FindRecords(sessionrepo.RecordQuery{})
	if err != nil || len(recs) != 0 {
		t.Fatalf("records %+v %v", recs, err)
	}
	log, err := s.GetLog(sessionrepo.LogOptions{})
	if err != nil || len(log) != 0 {
		t.Fatalf("log %+v %v", log, err)
	}
}

func TestConformanceConcurrentLaneWrites(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	if _, err := s.AppendEntry(sessionrepo.Entry{Type: "message", ID: "root", Message: userMsg("root")}, sessionrepo.MainLane); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateLane("thread", strPtr("root")); err != nil {
		t.Fatal(err)
	}
	type result struct {
		e   sessionrepo.Entry
		err error
	}
	ch := make(chan result, 4)
	writes := []struct {
		id   string
		lane string
	}{
		{"main-1", sessionrepo.MainLane},
		{"thread-1", "thread"},
		{"main-2", sessionrepo.MainLane},
		{"thread-2", "thread"},
	}
	var wg sync.WaitGroup
	for _, w := range writes {
		wg.Add(1)
		go func(id, lane string) {
			defer wg.Done()
			e, err := s.AppendEntry(sessionrepo.Entry{Type: "custom", ID: id, CustomType: "note"}, lane)
			ch <- result{e: e, err: err}
		}(w.id, w.lane)
	}
	wg.Wait()
	close(ch)
	var entries []sessionrepo.Entry
	for r := range ch {
		if r.err != nil {
			t.Fatal(r.err)
		}
		entries = append(entries, r.e)
	}
	seen := map[int64]struct{}{}
	for _, e := range entries {
		if _, ok := seen[e.Seq]; ok {
			t.Fatalf("duplicate seq %d", e.Seq)
		}
		seen[e.Seq] = struct{}{}
	}
	if len(seen) != 4 {
		t.Fatalf("seq set %v", seen)
	}
}

func TestConformanceTreeFork(t *testing.T) {
	repo, cwd := fixture(t)
	src := create(t, repo, "source", cwd)
	root, err := src.AppendMessage(userMsg("root"))
	if err != nil {
		t.Fatal(err)
	}
	if err := src.CreateLane("thread", strPtr(root)); err != nil {
		t.Fatal(err)
	}
	mainChild, err := src.AppendMessage(userMsg("main"))
	if err != nil {
		t.Fatal(err)
	}
	threadChild, err := src.View("thread").AppendMessage(userMsg("thread"))
	if err != nil {
		t.Fatal(err)
	}
	lab := "thread-tip"
	if err := src.SetLabel(threadChild, &lab); err != nil {
		t.Fatal(err)
	}
	meta, err := src.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	fork, err := repo.Fork(meta, sessionrepo.ForkOptions{Scope: "tree", CreateOptions: sessionrepo.CreateOptions{ID: "tree-fork", CWD: cwd}})
	if err != nil {
		t.Fatal(err)
	}
	ents, err := fork.FindEntries(sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(ents), []string{root, mainChild, threadChild})
	lanes, err := fork.GetLanes()
	if err != nil || len(lanes) != 2 {
		t.Fatalf("lanes %+v %v", lanes, err)
	}
	if lanes[0].LeafID == nil || *lanes[0].LeafID != mainChild || lanes[1].LeafID == nil || *lanes[1].LeafID != threadChild {
		t.Fatalf("lane leaves %+v", lanes)
	}
	got, err := fork.GetLabel(threadChild)
	if err != nil || got == nil || *got != "thread-tip" {
		t.Fatalf("label %v %v", got, err)
	}
	st, err := fork.GetStats()
	if err != nil || st.MessageCount != 3 {
		t.Fatalf("stats %+v %v", st, err)
	}
	log, err := fork.GetLog(sessionrepo.LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var laneItems []sessionrepo.LogItem
	for _, item := range log {
		if item.Kind == "lane" {
			laneItems = append(laneItems, item)
		}
	}
	if len(laneItems) != 2 || laneItems[0].Seq != 4 || laneItems[0].Lane != "main" {
		t.Fatalf("lane log %+v", laneItems)
	}
	if laneItems[1].Seq != 5 || laneItems[1].Lane != "thread" {
		t.Fatalf("lane log %+v", laneItems)
	}
}

func TestConformanceForkDefaultsAndAppend(t *testing.T) {
	repo, cwd := fixture(t)
	src := create(t, repo, "source", cwd)
	root, err := src.AppendMessage(userMsg("root"))
	if err != nil {
		t.Fatal(err)
	}
	tail, err := src.AppendMessage(userMsg("tail"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := src.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	before, err := repo.Fork(meta, sessionrepo.ForkOptions{Position: "before", CreateOptions: sessionrepo.CreateOptions{ID: "before-default-target", CWD: cwd}})
	if err != nil {
		t.Fatal(err)
	}
	ents, err := before.FindEntries(sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(ents), []string{root})
	at, err := repo.Fork(meta, sessionrepo.ForkOptions{Position: "at", CreateOptions: sessionrepo.CreateOptions{ID: "at-default-target", CWD: cwd}})
	if err != nil {
		t.Fatal(err)
	}
	ents, err = at.FindEntries(sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, ids(ents), []string{root, tail})
	if _, err := at.AppendMessage(userMsg("after fork")); err != nil {
		t.Fatal(err)
	}
	st, err := at.GetStats()
	if err != nil || st.MessageCount != 3 {
		t.Fatalf("stats %+v %v", st, err)
	}
}

func TestConformancePermanentLaneNames(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	if err := s.CreateLane("thread", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendRecord(opStarted("old-run", "thread", "run")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendRecord(sessionrepo.Record{
		Type: "queue_enqueued", ID: "old-next-run", Lane: "thread", Queue: "nextRun",
		Target: map[string]any{"type": "message", "id": "queued-message", "message": userMsg("queued")},
	}); err != nil {
		t.Fatal(err)
	}
	recs, err := s.FindRecords(sessionrepo.RecordQuery{Lane: "thread"})
	if err != nil {
		t.Fatal(err)
	}
	strsEq(t, recordIDs(recs), []string{"old-next-run", "old-run"})
	mustCode(t, s.CreateLane("thread", nil), sessionrepo.ErrAlreadyExists)
}

func recordIDs(recs []sessionrepo.Record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.ID
	}
	return out
}

func asSlice(v any) []any {
	if v == nil {
		return nil
	}
	s, _ := v.([]any)
	return s
}
