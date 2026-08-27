package sqlite

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

func TestWriterLeaseInvalidTiming(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.sqlite")
	_, err := NewRepository(Options{DatabasePath: path, WriterLease: &WriterLeaseOptions{TTLMs: 0, HeartbeatIntervalMs: 1}})
	errContains(t, err, "writerLease.ttlMs must be positive")
	_, err = NewRepository(Options{DatabasePath: path, WriterLease: &WriterLeaseOptions{TTLMs: 100, HeartbeatIntervalMs: 100}})
	errContains(t, err, "writerLease.heartbeatIntervalMs must be positive and less than ttlMs")
}

func TestWriterLeaseSharedQueueOnReopen(t *testing.T) {
	repo, cwd := fixture(t)
	s := create(t, repo, "session", cwd)
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := repo.Open(meta)
	if err != nil {
		t.Fatal(err)
	}
	var first, second string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		id, err := s.AppendMessage(userMsg("first"))
		if err != nil {
			t.Error(err)
			return
		}
		first = id
	}()
	go func() {
		defer wg.Done()
		id, err := reopened.AppendMessage(userMsg("second"))
		if err != nil {
			t.Error(err)
			return
		}
		second = id
	}()
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}
	ents, err := s.FindEntries(sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]struct{}{}
	for _, e := range ents {
		got[e.ID] = struct{}{}
	}
	if _, ok := got[first]; !ok {
		t.Fatalf("missing first %s in %v", first, ids(ents))
	}
	if _, ok := got[second]; !ok {
		t.Fatalf("missing second %s in %v", second, ids(ents))
	}
	if first == second {
		t.Fatal("same entry id")
	}
}

func TestWriterLeaseListDoesNotAcquire(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.sqlite")
	writer, err := NewRepository(Options{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	reader, err := NewRepository(Options{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	first, err := writer.Create(sessionrepo.CreateOptions{ID: "session-1", CWD: dir, Metadata: map[string]any{"profile": "reviewer"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := writer.Create(sessionrepo.CreateOptions{
		ID: "session-2", CWD: dir, ParentSessionID: "session-1",
		Metadata: map[string]any{"profile": "writer", "name": "application-owned name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	n1, n2 := "Review session", "Write session"
	if err := first.SetName(&n1); err != nil {
		t.Fatal(err)
	}
	if err := second.SetName(&n2); err != nil {
		t.Fatal(err)
	}
	firstMeta, err := first.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if firstMeta.Name != "Review session" || firstMeta.Metadata["profile"] != "reviewer" {
		t.Fatalf("first meta %+v", firstMeta)
	}
	db := openRaw(t, path)
	type leaseRow struct {
		sessionID, ownerID string
		fence, expires     int64
	}
	readLeases := func() []leaseRow {
		t.Helper()
		rows, err := db.Query(`SELECT session_id, owner_id, fence, expires_at_ms FROM writer_leases ORDER BY session_id`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []leaseRow
		for rows.Next() {
			var r leaseRow
			if err := rows.Scan(&r.sessionID, &r.ownerID, &r.fence, &r.expires); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := readLeases()
	listed, err := reader.List(sessionrepo.ListOptions{CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed %d", len(listed))
	}
	after := readLeases()
	if len(after) != len(before) {
		t.Fatalf("leases changed %v -> %v", before, after)
	}
	for i := range before {
		if after[i] != before[i] {
			t.Fatalf("leases changed %v -> %v", before, after)
		}
	}
	_, err = reader.Open(firstMeta)
	mustCode(t, err, sessionrepo.ErrStorage)
	errContains(t, err, "already has an active writer")
}

func TestWriterLeaseFenceTakeover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.sqlite")
	lease := &WriterLeaseOptions{TTLMs: 120_000, HeartbeatIntervalMs: 60_000}
	firstRepo, err := NewRepository(Options{DatabasePath: path, WriterLease: lease})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstRepo.Close() })
	secondRepo, err := NewRepository(Options{DatabasePath: path, WriterLease: lease})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondRepo.Close() })
	first := create(t, firstRepo, "session-1", dir)
	meta, err := first.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	db := openRaw(t, path)
	execSQL(t, db, `UPDATE writer_leases SET expires_at_ms = 0 WHERE session_id = ?`, meta.ID)
	second, err := secondRepo.Open(meta)
	if err != nil {
		t.Fatal(err)
	}
	_, err = first.AppendMessage(userMsg("stale owner"))
	mustCode(t, err, sessionrepo.ErrStorage)
	errContains(t, err, "writer lease was lost")
	ents, err := second.FindEntries(sessionrepo.EntryQuery{})
	if err != nil || len(ents) != 0 {
		t.Fatalf("second should be empty %+v %v", ents, err)
	}
	var owner string
	var fence int64
	if err := db.QueryRow(`SELECT owner_id, fence FROM writer_leases WHERE session_id = ?`, meta.ID).Scan(&owner, &fence); err != nil {
		t.Fatal(err)
	}
	if fence != 2 {
		t.Fatalf("fence %d", fence)
	}
	_ = firstRepo.Close()
	var ownerAfter string
	var fenceAfter int64
	if err := db.QueryRow(`SELECT owner_id, fence FROM writer_leases WHERE session_id = ?`, meta.ID).Scan(&ownerAfter, &fenceAfter); err != nil {
		t.Fatal(err)
	}
	if ownerAfter != owner || fenceAfter != fence {
		t.Fatalf("stale close dropped new lease %s/%d -> %s/%d", owner, fence, ownerAfter, fenceAfter)
	}
	if _, err := second.AppendMessage(userMsg("current owner")); err != nil {
		t.Fatal(err)
	}
}

func TestWriterLeaseConcurrentSessions(t *testing.T) {
	repo, cwd := fixture(t)
	first := create(t, repo, "session-1", cwd)
	second := create(t, repo, "session-2", cwd)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := first.AppendMessage(userMsg("first"))
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := second.AppendMessage(userMsg("second"))
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestWriterLeaseHeartbeatRenews(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.sqlite")
	repo, err := NewRepository(Options{DatabasePath: path, WriterLease: &WriterLeaseOptions{TTLMs: 400, HeartbeatIntervalMs: 80}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	s := create(t, repo, "session-1", dir)
	meta, err := s.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	db := openRaw(t, path)
	var initial int64
	if err := db.QueryRow(`SELECT expires_at_ms FROM writer_leases WHERE session_id = ?`, meta.ID).Scan(&initial); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var now int64
		if err := db.QueryRow(`SELECT expires_at_ms FROM writer_leases WHERE session_id = ?`, meta.ID).Scan(&now); err != nil {
			t.Fatal(err)
		}
		if now > initial {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("heartbeat did not renew expires_at_ms")
}

func TestWriterLeaseSecondWriterMessage(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "already has an active writer") {
		t.Fatalf("message %v", err)
	}
}
