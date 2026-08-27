package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

func TestMigrationsApplyOnceAndSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := applyMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := applyMigrations(db); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRow(`SELECT id FROM migrations ORDER BY id`).Scan(&id); err != nil || id != "001_initial.sql" {
		t.Fatalf("migration %q %v", id, err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("migration count %d %v", n, err)
	}
	required := []string{"migrations", "sessions", "entries", "session_sequences", "session_stats", "branch_entries", "branch_tips", "lanes", "records", "lane_moves", "facts", "writer_leases"}
	for _, name := range required {
		ok, err := tableExists(db, name)
		if err != nil || !ok {
			t.Fatalf("missing table %s: %v", name, err)
		}
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'leaf_id'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("sessions.leaf_id should not exist")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('lanes') WHERE name = 'open_operation_id'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("lanes.open_operation_id missing")
	}
	mustIndex(t, db, "sessions", "idx_sessions_cwd_created_at", true)
	mustIndex(t, db, "sessions", "idx_sessions_parent", false)
	mustIndex(t, db, "entries", "idx_entries_session_seq", false)
	mustIndex(t, db, "branch_entries", "idx_branch_entries_session_entry", true)
	mustIndex(t, db, "records", "idx_records_session_lane_seq", true)
	mustIndex(t, db, "records", "idx_records_session_type_seq", true)
	mustIndex(t, db, "records", "idx_records_session_type_op_kind_seq", true)
	mustIndex(t, db, "records", "idx_records_session_seq", false)
	mustIndex(t, db, "lane_moves", "idx_lane_moves_session_lane_seq", false)
}

func mustIndex(t *testing.T, db *sql.DB, table, name string, want bool) {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_index_list(?) WHERE name = ?`, table, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if want && n != 1 {
		t.Fatalf("missing index %s on %s", name, table)
	}
	if !want && n != 0 {
		t.Fatalf("forbidden index %s on %s", name, table)
	}
}

func TestRepositoryMetadataRoundTrip(t *testing.T) {
	repo, cwd := fixture(t)
	source, err := repo.Create(sessionrepo.CreateOptions{ID: "session-1", CWD: cwd, Metadata: map[string]any{"profile": "reviewer"}})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := source.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if meta.Metadata["profile"] != "reviewer" {
		t.Fatalf("create metadata %+v", meta.Metadata)
	}
	listed, err := repo.List(sessionrepo.ListOptions{CWD: cwd})
	if err != nil || len(listed) != 1 || listed[0].Metadata["profile"] != "reviewer" {
		t.Fatalf("list %+v %v", listed, err)
	}
	opened, err := repo.Open(meta)
	if err != nil {
		t.Fatal(err)
	}
	om, err := opened.GetMetadata()
	if err != nil || om.Metadata["profile"] != "reviewer" {
		t.Fatalf("open %+v %v", om, err)
	}
	fork, err := repo.Fork(meta, sessionrepo.ForkOptions{CreateOptions: sessionrepo.CreateOptions{ID: "session-2", CWD: cwd}})
	if err != nil {
		t.Fatal(err)
	}
	fm, err := fork.GetMetadata()
	if err != nil || fm.Metadata["profile"] != "reviewer" {
		t.Fatalf("fork %+v %v", fm, err)
	}
	over, err := repo.Fork(meta, sessionrepo.ForkOptions{CreateOptions: sessionrepo.CreateOptions{ID: "session-3", CWD: cwd, Metadata: map[string]any{"profile": "writer"}}})
	if err != nil {
		t.Fatal(err)
	}
	ov, err := over.GetMetadata()
	if err != nil || ov.Metadata["profile"] != "writer" {
		t.Fatalf("override %+v %v", ov, err)
	}
}

func TestRepositoryForkRollback(t *testing.T) {
	repo, cwd := fixture(t)
	source := create(t, repo, "source", cwd)
	if _, err := source.AppendMessage(userMsg("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := source.AppendMessage(assistantMsg("two")); err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `
CREATE TRIGGER fail_fork_entry BEFORE INSERT ON entries
WHEN new.session_id = 'fork' AND new.seq = 2
BEGIN
  SELECT RAISE(ABORT, 'fail fork');
END;`)
	meta, err := source.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Fork(meta, sessionrepo.ForkOptions{CreateOptions: sessionrepo.CreateOptions{ID: "fork", CWD: cwd}})
	mustCode(t, err, sessionrepo.ErrStorage)
	var n int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = 'fork'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("fork session leaked %d %v", n, err)
	}
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM entries WHERE session_id = 'fork'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("fork entries leaked %d %v", n, err)
	}
}

func TestRepositoryCorruptMetadataJSON(t *testing.T) {
	repo, cwd := fixture(t)
	create(t, repo, "session-meta", cwd)
	execSQL(t, repo.db, `UPDATE sessions SET metadata = ? WHERE id = ?`, "not json", "session-meta")
	_, err := repo.List(sessionrepo.ListOptions{})
	mustCode(t, err, sessionrepo.ErrStorage)
	errContains(t, err, "metadata is not valid JSON")
}

func TestRepositoryCorruptMetadataNonObject(t *testing.T) {
	repo, cwd := fixture(t)
	create(t, repo, "session-meta-obj", cwd)
	execSQL(t, repo.db, `UPDATE sessions SET metadata = ? WHERE id = ?`, "[]", "session-meta-obj")
	_, err := repo.List(sessionrepo.ListOptions{CWD: cwd})
	mustCode(t, err, sessionrepo.ErrStorage)
	errContains(t, err, "metadata must be an object")
}

func TestRepositoryCorruptNameJSON(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-name", cwd)
	nm := "valid name"
	if err := s.SetName(&nm); err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `UPDATE facts SET value = ? WHERE session_id = ? AND kind = 'name'`, "not json", "session-name")
	_, err := repo.List(sessionrepo.ListOptions{CWD: cwd})
	mustCode(t, err, sessionrepo.ErrStorage)
	errContains(t, err, "name is not valid JSON")
}

func TestRepositoryCorruptNameNonString(t *testing.T) {
	repo, cwd := fixture(t)
	named := create(t, repo, "session-name-obj", cwd)
	nm := "valid name"
	if err := named.SetName(&nm); err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `UPDATE facts SET value = ? WHERE session_id = ? AND kind = 'name'`, "{}", "session-name-obj")
	_, err := named.GetMetadata()
	mustCode(t, err, sessionrepo.ErrStorage)
	errContains(t, err, "name must be a string")
}

func TestRepositoryMissingLaneLeaf(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `UPDATE lanes SET leaf_id = ? WHERE session_id = ? AND lane = ?`, "missing", meta.ID, "main")
	_, err = s.GetLanes()
	mustCode(t, err, sessionrepo.ErrStorage)
	errContains(t, err, "Lane main points at missing entry missing")
	_, err = repo.Open(meta)
	mustCode(t, err, sessionrepo.ErrStorage)
	errContains(t, err, "Lane main points at missing entry missing")
}

func TestRepositoryCorruptEntryAndRecord(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	entryID, err := s.AppendMessage(userMsg("message"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `UPDATE entries SET payload = ? WHERE session_id = ? AND id = ?`, "not json", meta.ID, entryID)
	reopened, err := repo.Open(meta)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reopened.FindEntries(sessionrepo.EntryQuery{})
	mustCode(t, err, sessionrepo.ErrInvalidEntry)

	s = create(t, repo, "session-2", cwd)
	if _, err := s.AppendRecord(sessionrepo.Record{
		Type: "operation_finished", ID: "record-1", Lane: sessionrepo.MainLane, RunID: "run-1", HasRunID: true, Outcome: "completed",
	}); err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `UPDATE records SET payload = ? WHERE session_id = ? AND id = ?`, "not json", "session-2", "record-1")
	_, err = s.FindRecords(sessionrepo.RecordQuery{})
	mustCode(t, err, sessionrepo.ErrStorage)
	errContains(t, err, "failed to decode payload")
}

func TestRepositoryAppendFailureDoesNotPublish(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	execSQL(t, repo.db, `
CREATE TRIGGER fail_branch_tip_insert
BEFORE INSERT ON branch_tips
BEGIN
  SELECT RAISE(ABORT, 'branch insert failed');
END;`)
	_, err := s.AppendMessage(userMsg("root"))
	if err == nil {
		t.Fatal("expected append failure")
	}
	errContains(t, err, "branch insert failed")
	var leaf sql.NullString
	if err := repo.db.QueryRow(`SELECT leaf_id FROM lanes WHERE session_id = ? AND lane = ?`, "session-1", "main").Scan(&leaf); err != nil {
		t.Fatal(err)
	}
	if leaf.Valid {
		t.Fatalf("leaf published: %s", leaf.String)
	}
	var n int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM entries WHERE session_id = ?`, "session-1").Scan(&n); err != nil || n != 0 {
		t.Fatalf("entries leaked %d %v", n, err)
	}
	st, err := s.GetStats()
	if err != nil || st.MessageCount != 0 {
		t.Fatalf("stats %+v %v", st, err)
	}
	execSQL(t, repo.db, `DROP TRIGGER fail_branch_tip_insert`)
	id, err := s.AppendMessage(userMsg("root"))
	if err != nil {
		t.Fatal(err)
	}
	ents, err := s.FindEntries(sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst})
	if err != nil || len(ents) != 1 || ents[0].ID != id {
		t.Fatalf("after recovery %+v %v", ents, err)
	}
}

func TestRepositoryUsageFromAssistantCompactionBranchSummary(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	userID, err := s.AppendMessage(userMsg("one"))
	if err != nil {
		t.Fatal(err)
	}
	asst := assistantMsg("two")
	asst["provider"] = "anthropic"
	asst["model"] = "claude-sonnet-4-5"
	asst["usage"] = map[string]any{
		"input": 100, "output": 25, "cacheRead": 40, "cacheWrite": 10, "totalTokens": 175,
		"cost": map[string]any{"input": 0.1, "output": 0.2, "cacheRead": 0.03, "cacheWrite": 0.04, "total": 0.37},
	}
	assistantID, err := s.AppendMessage(asst)
	if err != nil {
		t.Fatal(err)
	}
	u := usage(100, 25, 40, 10, 175, 0.37)
	if _, err := s.AppendRecord(sessionrepo.Record{
		Type: "usage", ID: "assistant-usage", Lane: sessionrepo.MainLane, Cause: "assistant",
		RunID: "run", HasRunID: true, EntryID: assistantID, Attempt: 1, StopReason: "stop", Usage: &u,
	}); err != nil {
		t.Fatal(err)
	}
	compactionUsage := usage(1, 2, 3, 4, 10, 0.1)
	compactionID, err := s.AppendEntry(sessionrepo.Entry{
		Type: "compaction", ID: "compact", Summary: "summary", RetainedTail: []any{}, TokensBefore: 200,
		HasTokensBefore: true, Usage: &compactionUsage,
	}, sessionrepo.MainLane)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendRecord(sessionrepo.Record{
		Type: "usage", ID: "compaction-usage", Lane: sessionrepo.MainLane, Cause: "compaction",
		RunID: "run", HasRunID: true, EntryID: compactionID.ID, Attempt: 1, StopReason: "stop", Usage: &compactionUsage,
	}); err != nil {
		t.Fatal(err)
	}
	branchUsage := usage(5, 6, 7, 8, 26, 0.26)
	if err := s.MoveLane(sessionrepo.MainLane, strPtr(userID)); err != nil {
		t.Fatal(err)
	}
	branch, err := s.AppendEntry(sessionrepo.Entry{
		Type: "branch_summary", ID: "branch", FromID: userID, Summary: "branch summary", Usage: &branchUsage,
	}, sessionrepo.MainLane)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendRecord(sessionrepo.Record{
		Type: "usage", ID: "branch-summary-usage", Lane: sessionrepo.MainLane, Cause: "branch_summary",
		RunID: "run", HasRunID: true, EntryID: branch.ID, Attempt: 1, StopReason: "stop", Usage: &branchUsage,
	}); err != nil {
		t.Fatal(err)
	}
	st, err := s.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.MessageCount != 2 || st.CachedTokens != 50 || st.UncachedTokens != 128 || st.TotalTokens != 211 {
		t.Fatalf("stats %+v", st)
	}
	if st.CostTotal < 0.72 || st.CostTotal > 0.74 {
		t.Fatalf("cost %v", st.CostTotal)
	}
}
