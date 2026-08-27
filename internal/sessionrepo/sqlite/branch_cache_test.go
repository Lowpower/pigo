package sqlite

import (
	"testing"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

func TestBranchCacheCompletePathAfterCompaction(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	rootID, err := s.AppendMessage(userMsg("root"))
	if err != nil {
		t.Fatal(err)
	}
	keptID, err := s.AppendMessage(userMsg("kept"))
	if err != nil {
		t.Fatal(err)
	}
	compactionID := appendCompaction(t, s, "c1", "summary", 100)
	if _, err := s.AppendMessage(assistantMsg("first child")); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveLane(sessionrepo.MainLane, strPtr(compactionID)); err != nil {
		t.Fatal(err)
	}
	branchedID, err := s.AppendMessage(assistantMsg("branched child"))
	if err != nil {
		t.Fatal(err)
	}
	var branchID string
	if err := repo.db.QueryRow(`SELECT branch_id FROM branch_entries WHERE session_id = ? AND entry_id = ?`, "session-1", branchedID).Scan(&branchID); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.db.Query(`SELECT entry_id FROM branch_entries WHERE session_id = ? AND branch_id = ? ORDER BY entry_seq`, "session-1", branchID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	strsEq(t, got, []string{rootID, keptID, compactionID, branchedID})
}

func TestBranchCacheCompactedWindowFromCorruptPayload(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	oldID, err := s.AppendMessage(userMsg("old"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendMessage(userMsg("kept")); err != nil {
		t.Fatal(err)
	}
	compactionID := appendCompaction(t, s, "c1", "summary", 100)
	leafID, err := s.AppendMessage(assistantMsg("new"))
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `UPDATE entries SET payload = ? WHERE session_id = ? AND id = ?`, "not json", "session-1", oldID)
	br := getBranch(t, s)
	strsEq(t, ids(br), []string{compactionID, leafID})
}

func TestBranchCacheNestedCompaction(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	if _, err := s.AppendMessage(userMsg("root")); err != nil {
		t.Fatal(err)
	}
	appendCompaction(t, s, "c1", "first summary", 100)
	if _, err := s.AppendMessage(userMsg("middle")); err != nil {
		t.Fatal(err)
	}
	second := appendCompaction(t, s, "c2", "second summary", 200)
	leafID, err := s.AppendMessage(assistantMsg("new"))
	if err != nil {
		t.Fatal(err)
	}
	br := getBranch(t, s)
	strsEq(t, ids(br), []string{second, leafID})
}

func TestBranchCacheMissingNoAutoRepair(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	if _, err := s.AppendMessage(userMsg("root")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendMessage(assistantMsg("child")); err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `DELETE FROM branch_tips WHERE session_id = ?`, "session-1")
	execSQL(t, repo.db, `DELETE FROM branch_entries WHERE session_id = ?`, "session-1")
	_, err := s.FindEntriesOnBranch(sessionrepo.EntryQuery{})
	mustCode(t, err, sessionrepo.ErrInvalidEntry)
	_, err = s.AppendMessage(assistantMsg("later"))
	mustCode(t, err, sessionrepo.ErrInvalidEntry)
	errContains(t, err, "has no branch containing parent entry")
	var n int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM branch_entries WHERE session_id = ?`, "session-1").Scan(&n); err != nil || n != 0 {
		t.Fatalf("auto-repaired cache: %d %v", n, err)
	}
}

func TestBranchCacheExplicitRepair(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	rootID, err := s.AppendMessage(userMsg("root"))
	if err != nil {
		t.Fatal(err)
	}
	childID, err := s.AppendMessage(assistantMsg("child"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `DELETE FROM branch_tips WHERE session_id = ?`, "session-1")
	execSQL(t, repo.db, `DELETE FROM branch_entries WHERE session_id = ?`, "session-1")
	_, err = s.FindEntriesOnBranch(sessionrepo.EntryQuery{})
	mustCode(t, err, sessionrepo.ErrInvalidEntry)
	if err := repo.RepairBranchCache(meta); err != nil {
		t.Fatal(err)
	}
	br := getBranch(t, s)
	strsEq(t, ids(br), []string{rootID, childID})
}

func TestBranchCacheForkRequiresCache(t *testing.T) {
	repo, cwd := fixture(t)
	source := create(t, repo, "source", cwd)
	rootID, err := source.AppendMessage(userMsg("root"))
	if err != nil {
		t.Fatal(err)
	}
	childID, err := source.AppendMessage(assistantMsg("child"))
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `DELETE FROM branch_tips WHERE session_id = ?`, "source")
	execSQL(t, repo.db, `DELETE FROM branch_entries WHERE session_id = ?`, "source")
	if rootID == childID {
		t.Fatal("expected distinct ids")
	}
	meta, err := source.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Fork(meta, sessionrepo.ForkOptions{
		HasEntry: true, EntryID: childID, Position: "at",
		CreateOptions: sessionrepo.CreateOptions{ID: "fork", CWD: cwd},
	})
	mustCode(t, err, sessionrepo.ErrInvalidForkTarget)
}

func TestBranchCacheStaleParent(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	rootID, err := s.AppendMessage(userMsg("root"))
	if err != nil {
		t.Fatal(err)
	}
	staleID, err := s.AppendMessage(assistantMsg("stale"))
	if err != nil {
		t.Fatal(err)
	}
	leafID, err := s.AppendMessage(userMsg("leaf"))
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, repo.db, `UPDATE entries SET parent_id = ? WHERE session_id = ? AND id = ?`, rootID, "session-1", leafID)
	if staleID == leafID {
		t.Fatal("expected distinct ids")
	}
	_, err = s.FindEntriesOnBranch(sessionrepo.EntryQuery{Start: leafID, Order: sessionrepo.OrderOldestFirst})
	mustCode(t, err, sessionrepo.ErrInvalidEntry)
}

func TestBranchCacheDeletedWithSession(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session-1", cwd)
	if _, err := s.AppendMessage(userMsg("root")); err != nil {
		t.Fatal(err)
	}
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(meta); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM branch_entries WHERE session_id = ?`, "session-1").Scan(&n); err != nil || n != 0 {
		t.Fatalf("branch_entries %d %v", n, err)
	}
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM branch_tips WHERE session_id = ?`, "session-1").Scan(&n); err != nil || n != 0 {
		t.Fatalf("branch_tips %d %v", n, err)
	}
}
