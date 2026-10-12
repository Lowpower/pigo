package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func symlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skip("symlink:", err)
	}
}

func writeJSONL(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListAllFollowsSymlinkedSessionsRoot(t *testing.T) {
	agent := t.TempDir()
	realSessions := filepath.Join(t.TempDir(), "sessions")
	target := filepath.Join(realSessions, "--proj--", "a.jsonl")
	writeJSONL(t, target)
	link := filepath.Join(agent, "sessions")
	symlinkOrSkip(t, realSessions, link)

	got, err := ListAll(agent)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(link, "--proj--", "a.jsonl")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v want %s", got, want)
	}
}

func TestListAllFollowsSymlinkedProjectDir(t *testing.T) {
	agent := t.TempDir()
	sessions := filepath.Join(agent, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "linked.jsonl")
	writeJSONL(t, target)
	alias := filepath.Join(sessions, "--linked--")
	symlinkOrSkip(t, filepath.Dir(target), alias)

	got, err := ListAll(agent)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(alias, "linked.jsonl")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v want %s", got, want)
	}
}

func TestListAllDedupesDirectoryAliases(t *testing.T) {
	agent := t.TempDir()
	sessions := filepath.Join(agent, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	targetDir := t.TempDir()
	writeJSONL(t, filepath.Join(targetDir, "same.jsonl"))
	symlinkOrSkip(t, targetDir, filepath.Join(sessions, "--a--"))
	symlinkOrSkip(t, targetDir, filepath.Join(sessions, "--b--"))

	got, err := ListAll(agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	want := filepath.Join(sessions, "--a--", "same.jsonl")
	if got[0] != want {
		t.Fatalf("path = %s want %s", got[0], want)
	}
}

func TestListAllSymlinkCycle(t *testing.T) {
	agent := t.TempDir()
	sessions := filepath.Join(agent, "sessions")
	regular := filepath.Join(sessions, "--regular--", "ok.jsonl")
	writeJSONL(t, regular)
	a := filepath.Join(sessions, "--a--")
	b := filepath.Join(sessions, "--b--")
	symlinkOrSkip(t, b, a)
	symlinkOrSkip(t, a, b)

	done := make(chan struct{})
	var got []string
	var err error
	go func() {
		got, err = ListAll(agent)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("symlink cycle did not finish")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != regular {
		t.Fatalf("got %v", got)
	}
}

func TestListAllIgnoresBrokenAndFileLinks(t *testing.T) {
	agent := t.TempDir()
	sessions := filepath.Join(agent, "sessions")
	regular := filepath.Join(sessions, "--regular--", "regular.jsonl")
	writeJSONL(t, regular)

	removed := filepath.Join(t.TempDir(), "removed")
	if err := os.MkdirAll(removed, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, removed, filepath.Join(sessions, "--broken--"))
	if err := os.RemoveAll(removed); err != nil {
		t.Fatal(err)
	}

	targetFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(targetFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, targetFile, filepath.Join(sessions, "--file--"))

	got, err := ListAll(agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != regular {
		t.Fatalf("got %v", got)
	}
}

func TestListDedupesFileSymlink(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, agent)
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(m.File()), "alias.jsonl")
	symlinkOrSkip(t, m.File(), alias)

	got, err := List(cwd, agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !SamePath(got[0], m.File()) {
		t.Fatalf("got %v", got)
	}
}

func TestDeleteFileRefusesSymlinkAliasOfCurrent(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, current)
	alias := filepath.Join(dir, "alias.jsonl")
	symlinkOrSkip(t, current, alias)

	if !SamePath(alias, current) {
		t.Fatal("alias and target should be the same path")
	}
	if err := DeleteFile(alias, current); err == nil {
		t.Fatal("must not delete the current session via an alias")
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.jsonl")
	writeJSONL(t, other)
	if err := DeleteFile(other, current); err != nil {
		t.Fatal(err)
	}
}

func TestBuildThreadMatchesSymlinkedParent(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent.jsonl")
	writeJSONL(t, parent)
	alias := filepath.Join(dir, "parent-alias.jsonl")
	symlinkOrSkip(t, parent, alias)
	child := filepath.Join(dir, "child.jsonl")
	writeJSONL(t, child)

	rows := BuildThread([]Summary{
		{Path: alias, ID: "p", Modified: time.Now().Add(-time.Hour)},
		{Path: child, ID: "c", ParentSession: parent, Modified: time.Now()},
	})
	if len(rows) != 2 || rows[0].ID != "p" || rows[1].ID != "c" || !strings.Contains(rows[1].Prefix, "└─") {
		t.Fatalf("tree = %+v", rows)
	}
}
