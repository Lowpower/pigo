package sqlite

import (
	"database/sql"
	"testing"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

func errContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if msg := err.Error(); !contains(msg, substr) {
		t.Fatalf("error %q does not contain %q", msg, substr)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		t.Fatal(err)
	}
	return db
}

func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func appendCompaction(t *testing.T, s *sessionrepo.Session, id, summary string, tokens float64) string {
	t.Helper()
	e, err := s.AppendEntry(sessionrepo.Entry{
		Type:            "compaction",
		ID:              id,
		Summary:         summary,
		RetainedTail:    []any{},
		TokensBefore:    tokens,
		HasTokensBefore: true,
	}, sessionrepo.MainLane)
	if err != nil {
		t.Fatal(err)
	}
	return e.ID
}

func getBranch(t *testing.T, s *sessionrepo.Session) []sessionrepo.Entry {
	t.Helper()
	leaf, err := s.GetLeafID()
	if err != nil {
		t.Fatal(err)
	}
	if leaf == nil {
		return nil
	}
	ents, err := s.FindEntriesOnBranch(sessionrepo.EntryQuery{Start: *leaf, StopAtType: "compaction"})
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(ents)-1; i < j; i, j = i+1, j-1 {
		ents[i], ents[j] = ents[j], ents[i]
	}
	return ents
}

func usage(in, out, cacheRead, cacheWrite, total, cost float64) sessionrepo.Usage {
	return sessionrepo.Usage{
		Input: in, Output: out, CacheRead: cacheRead, CacheWrite: cacheWrite, TotalTokens: total,
		Cost: sessionrepo.UsageCost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: cost},
	}
}
