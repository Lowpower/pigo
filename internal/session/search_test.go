package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSearchQuery(t *testing.T) {
	q := ParseSearchQuery(`foo "node cve" bar`)
	if q.Error != "" || len(q.Tokens) != 3 {
		t.Fatalf("%+v", q)
	}
	if q.Tokens[1].Kind != "phrase" || q.Tokens[1].Value != "node cve" {
		t.Fatalf("phrase = %+v", q.Tokens[1])
	}
	re := ParseSearchQuery("re:haiku")
	if re.Regex == nil {
		t.Fatalf("regex: %+v", re)
	}
	bad := ParseSearchQuery("re:[")
	if bad.Error == "" {
		t.Fatal("invalid regex should error")
	}
}

func TestFilterSessionsPhraseAndRegex(t *testing.T) {
	now := time.Now()
	list := []Summary{
		{ID: "a", Name: "alpha", FirstMessage: "hello world", SearchText: "a alpha hello world", Modified: now},
		{ID: "b", FirstMessage: "node cve notes", SearchText: "b node cve notes", Modified: now.Add(-time.Hour)},
	}
	got := FilterSessions(list, `"node cve"`, SortRecent, NameAll)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("phrase = %+v", got)
	}
	got = FilterSessions(list, "re:hel+o", SortFuzzy, NameAll)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("regex = %+v", got)
	}
	got = FilterSessions(list, "re:[", SortFuzzy, NameAll)
	if len(got) != 0 {
		t.Fatalf("bad regex should be empty, got %+v", got)
	}
	got = FilterSessions(list, "", SortRecent, NameNamed)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("named = %+v", got)
	}
}

func TestBuildThread(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent.jsonl")
	child := filepath.Join(dir, "child.jsonl")
	list := []Summary{
		{Path: parent, ID: "p", FirstMessage: "root", Modified: time.Now().Add(-time.Hour)},
		{Path: child, ID: "c", FirstMessage: "kid", ParentSession: parent, Modified: time.Now()},
	}
	rows := BuildThread(list)
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0].ID != "p" || rows[1].ID != "c" || !strings.Contains(rows[1].Prefix, "└─") && !strings.Contains(rows[1].Prefix, "├─") {
		t.Fatalf("tree = %+v", rows)
	}
}

func TestNextSort(t *testing.T) {
	if NextSort(SortThreaded) != SortRecent || NextSort(SortRecent) != SortFuzzy || NextSort(SortFuzzy) != SortThreaded {
		t.Fatal(NextSort(SortThreaded), NextSort(SortRecent), NextSort(SortFuzzy))
	}
}

func TestListAllAndDeleteAndRename(t *testing.T) {
	agent := t.TempDir()
	cwdA := t.TempDir()
	cwdB := t.TempDir()
	a := mustSession(t, cwdA, agent, "one")
	b := mustSession(t, cwdB, agent, "two")
	all, err := SummariesAll(agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all=%d", len(all))
	}
	if err := UpdateHeader(a.File(), func(h *Header) { h.Name = "named-a" }); err != nil {
		t.Fatal(err)
	}
	h, _, err := Load(a.File())
	if err != nil || h.Name != "named-a" {
		t.Fatalf("name=%q err=%v", h.Name, err)
	}
	if err := DeleteFile(a.File(), b.File()); err != nil {
		t.Fatal(err)
	}
	if err := DeleteFile(b.File(), b.File()); err == nil {
		t.Fatal("must not delete the current session")
	}
	if _, err := os.Stat(a.File()); !os.IsNotExist(err) {
		t.Fatal("expected deleted")
	}
}

func mustSession(t *testing.T, cwd, agent, text string) *Manager {
	t.Helper()
	m := New(cwd, agent)
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": text}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFormatAge(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	if got := FormatAge(now, now); got != "now" {
		t.Fatal(got)
	}
	if got := FormatAge(now.Add(-5*time.Minute), now); got != "5m" {
		t.Fatal(got)
	}
}
