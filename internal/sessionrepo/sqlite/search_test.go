package sqlite

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

func TestSearchTrigramsAndMetadata(t *testing.T) {
	repo, cwd := fixture(t)
	search, err := NewSearch(repo.absPath)
	if err != nil {
		t.Fatal(err)
	}
	included, err := repo.Create(sessionrepo.CreateOptions{ID: "included", CWD: cwd, Metadata: map[string]any{"name": "application-owned"}})
	if err != nil {
		t.Fatal(err)
	}
	excluded, err := repo.Create(sessionrepo.CreateOptions{ID: "excluded", CWD: cwd + "/other"})
	if err != nil {
		t.Fatal(err)
	}
	entryID, err := included.AppendMessage(userMsg("Find the auth defect"))
	if err != nil {
		t.Fatal(err)
	}
	name := "Canonical name"
	if err := included.SetName(&name); err != nil {
		t.Fatal(err)
	}
	excludedID, err := excluded.AppendMessage(userMsg("Find the auth defect"))
	if err != nil {
		t.Fatal(err)
	}
	authHits, err := search.Search(t.Context(), "auth", sessionrepo.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(authHits) != 2 {
		t.Fatalf("auth hits %d %+v", len(authHits), authHits)
	}
	bySession := map[string]sessionrepo.SearchHit{}
	for _, h := range authHits {
		bySession[h.SessionID] = h
	}
	inc := bySession["included"]
	if inc.EntryID != entryID || inc.Metadata.Name != "Canonical name" {
		t.Fatalf("included %+v", inc)
	}
	if inc.Metadata.Metadata["name"] != "application-owned" {
		t.Fatalf("opaque metadata %+v", inc.Metadata.Metadata)
	}
	if bySession["excluded"].EntryID != excludedID {
		t.Fatalf("excluded %+v", bySession["excluded"])
	}
	uth, err := search.Search(t.Context(), "uth", sessionrepo.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(uth) != 2 {
		t.Fatalf("trigram uth hits %d %+v", len(uth), uth)
	}
}

func TestSearchOmitsClearedName(t *testing.T) {
	repo, cwd := fixture(t)
	search, err := NewSearch(repo.absPath)
	if err != nil {
		t.Fatal(err)
	}
	s := create(t, repo, "session-1", cwd)
	entryID, err := s.AppendMessage(userMsg("Find the auth defect"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := "Temporary"
	if err := s.SetName(&tmp); err != nil {
		t.Fatal(err)
	}
	if err := s.SetName(nil); err != nil {
		t.Fatal(err)
	}
	hits, err := search.Search(t.Context(), "auth", sessionrepo.SearchOptions{})
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits %+v %v", hits, err)
	}
	if hits[0].EntryID != entryID || hits[0].Metadata.HasName || hits[0].Metadata.Name != "" {
		t.Fatalf("cleared name still present %+v", hits[0].Metadata)
	}
	raw, err := json.Marshal(hits[0].Metadata)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["name"]; ok {
		t.Fatalf("json still has name: %s", raw)
	}
}

func TestSearchQuotedTextAndFilters(t *testing.T) {
	repo, cwd := fixture(t)
	search, err := NewSearch(repo.absPath)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := search.Search(t.Context(), `missing "phrase"`, sessionrepo.SearchOptions{})
	if err != nil || len(hits) != 0 {
		t.Fatalf("quoted %+v %v", hits, err)
	}
	s := create(t, repo, "session-1", cwd)
	msgID, err := s.AppendMessage(userMsg("Find the auth defect"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendCustomEntry("note", map[string]any{"text": "Find the auth custom entry"}, true); err != nil {
		t.Fatal(err)
	}
	filtered, err := search.Search(t.Context(), "auth", sessionrepo.SearchOptions{EntryTypes: []string{"message"}})
	if err != nil || len(filtered) != 1 || filtered[0].EntryID != msgID {
		t.Fatalf("type filter %+v %v", filtered, err)
	}
	s2 := create(t, repo, "session-2", cwd)
	if _, err := s2.AppendMessage(userMsg("Find the auth defect too")); err != nil {
		t.Fatal(err)
	}
	limited, err := search.Search(t.Context(), "auth", sessionrepo.SearchOptions{Limit: 1, HasLimit: true})
	if err != nil || len(limited) != 1 {
		t.Fatalf("limit 1 %+v %v", limited, err)
	}
	none, err := search.Search(t.Context(), "auth", sessionrepo.SearchOptions{Limit: 0, HasLimit: true})
	if err != nil || len(none) != 0 {
		t.Fatalf("limit 0 %+v %v", none, err)
	}
}

func TestSearchRebuildDeleteAndTriggers(t *testing.T) {
	repo, cwd := fixture(t)
	search, err := NewSearch(repo.absPath)
	if err != nil {
		t.Fatal(err)
	}
	s := create(t, repo, "session-1", cwd)
	entryID, err := s.AppendMessage(userMsg("Find the auth defect"))
	if err != nil {
		t.Fatal(err)
	}
	hits, err := search.Search(t.Context(), "auth", sessionrepo.SearchOptions{})
	if err != nil || len(hits) != 1 || hits[0].EntryID != entryID {
		t.Fatalf("rebuild %+v %v", hits, err)
	}
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(meta); err != nil {
		t.Fatal(err)
	}
	hits, err = search.Search(t.Context(), "auth", sessionrepo.SearchOptions{})
	if err != nil || len(hits) != 0 {
		t.Fatalf("after delete %+v %v", hits, err)
	}

	s = create(t, repo, "session-2", cwd)
	entryID, err = s.AppendMessage(userMsg("Find the auth defect"))
	if err != nil {
		t.Fatal(err)
	}
	hits, err = search.Search(t.Context(), "auth", sessionrepo.SearchOptions{})
	if err != nil || len(hits) != 1 {
		t.Fatalf("post-init insert %+v %v", hits, err)
	}
	db := openRaw(t, repo.absPath)
	execSQL(t, db, `DELETE FROM entries WHERE session_id = ? AND id = ?`, "session-2", entryID)
	hits, err = search.Search(t.Context(), "auth", sessionrepo.SearchOptions{})
	if err != nil || len(hits) != 0 {
		t.Fatalf("after row delete %+v %v", hits, err)
	}
}

func TestSearchBlankDoesNotCreateFTS(t *testing.T) {
	repo, cwd := fixture(t)
	search, err := NewSearch(repo.absPath)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := search.Search(t.Context(), "  ", sessionrepo.SearchOptions{})
	if err != nil || len(hits) != 0 {
		t.Fatalf("blank %+v %v", hits, err)
	}
	s := create(t, repo, "session-1", cwd)
	ok, err := tableExists(repo.db, "session_search_fts")
	if err != nil || ok {
		t.Fatalf("fts should not exist: %v %v", ok, err)
	}
	if _, err := s.AppendMessage(userMsg("still writable")); err != nil {
		t.Fatal(err)
	}
}

func TestSearchFTSFailureRollsBackAppendAndDelete(t *testing.T) {
	repo, cwd := fixture(t)
	search, err := NewSearch(repo.absPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := search.Search(t.Context(), "initialize", sessionrepo.SearchOptions{}); err != nil {
		t.Fatal(err)
	}
	s := create(t, repo, "session-1", cwd)
	db := openRaw(t, repo.absPath)
	execSQL(t, db, `DROP TABLE session_search_fts`)
	_, err = s.AppendMessage(userMsg("must roll back"))
	if err == nil {
		t.Fatal("append should fail after FTS drop")
	}
	ents, err := s.FindEntries(sessionrepo.EntryQuery{})
	if err != nil || len(ents) != 0 {
		t.Fatalf("canonical append leaked %+v %v", ents, err)
	}

	if _, err := search.Search(t.Context(), "initialize", sessionrepo.SearchOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendMessage(userMsg("must remain")); err != nil {
		t.Fatal(err)
	}
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, `DROP TABLE session_search_fts`)
	if err := repo.Delete(meta); err == nil {
		t.Fatal("delete should fail after FTS drop")
	}
	reopened, err := repo.Open(meta)
	if err != nil {
		t.Fatal(err)
	}
	ents, err = reopened.FindEntries(sessionrepo.EntryQuery{})
	if err != nil || len(ents) != 1 {
		t.Fatalf("delete leaked %+v %v", ents, err)
	}
}

func TestSearchBeforeFirstSession(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sessions.sqlite"
	repo, err := NewRepository(Options{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	search, err := NewSearch(path)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := search.Search(t.Context(), "auth", sessionrepo.SearchOptions{})
	if err != nil || len(hits) != 0 {
		t.Fatalf("empty search %+v %v", hits, err)
	}
	s := create(t, repo, "session-1", dir)
	entryID, err := s.AppendMessage(userMsg("Find the auth defect"))
	if err != nil {
		t.Fatal(err)
	}
	hits, err = search.Search(t.Context(), "auth", sessionrepo.SearchOptions{})
	if err != nil || len(hits) != 1 || hits[0].EntryID != entryID {
		t.Fatalf("after create %+v %v", hits, err)
	}
	if _, err := s.AppendMessage(userMsg("Still writable")); err != nil {
		t.Fatal(err)
	}
}

func TestSearchSetupFailureClosesDB(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sessions.sqlite"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE migrations (id TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO migrations (id, applied_at) VALUES ('001_initial.sql', 'now')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	search, err := NewSearch(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = search.Search(t.Context(), "auth", sessionrepo.SearchOptions{})
	if err == nil {
		t.Fatal("expected search setup failure without canonical tables")
	}
}
