package session

import (
	"os"
	"strings"
	"testing"
)

func TestGetTreeLinearAndBranch(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, dir)
	u1, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "one"})
	a1, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	u2, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "two"})
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok2"}); err != nil {
		t.Fatal(err)
	}
	tree := m.GetTree()
	if len(tree) != 1 || len(tree[0].Children) != 1 {
		t.Fatalf("linear tree = %+v", tree)
	}
	if err := m.Branch(a1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "alt"}); err != nil {
		t.Fatal(err)
	}
	tree = m.GetTree()
	if len(tree) != 1 {
		t.Fatalf("roots = %d", len(tree))
	}
	asst := tree[0].Children[0]
	if asst.Entry.ID != a1.ID {
		t.Fatalf("assistant node = %s want %s", asst.Entry.ID, a1.ID)
	}
	if len(asst.Children) != 2 {
		t.Fatalf("branch children = %d, want 2 (two + alt); ids=%v", len(asst.Children), childIDs(asst))
	}
	_ = u1
	_ = u2
}

func childIDs(n TreeNode) []string {
	var ids []string
	for _, c := range n.Children {
		ids = append(ids, c.Entry.ID)
	}
	return ids
}

func TestCreateBranchedSessionKeepsPath(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, dir)
	u, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	a, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"})
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "later"}); err != nil {
		t.Fatal(err)
	}
	child, err := m.CreateBranchedSession(a.ID, cwd, dir)
	if err != nil {
		t.Fatal(err)
	}
	if child.header.ParentSession != m.File() {
		t.Fatalf("parentSession = %q want file %q", child.header.ParentSession, m.File())
	}
	got := child.Entries()
	if len(got) != 2 || got[0].ID != u.ID || got[1].ID != a.ID {
		t.Fatalf("branched entries = %+v", got)
	}
	if _, err := os.Stat(child.File()); err != nil {
		t.Fatal(err)
	}
}

func TestForkFromBeforeUserMessage(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, dir)
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "first"})
	a, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	u2, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "second"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok2"})
	child, text, err := m.ForkFrom(u2.ID, cwd, dir, "before")
	if err != nil {
		t.Fatal(err)
	}
	if text != "second" {
		t.Fatalf("text = %q", text)
	}
	got := child.Entries()
	if len(got) != 2 || got[1].ID != a.ID {
		t.Fatalf("forked = %+v", got)
	}
}

func TestFormatTreeAndExportHTML(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, dir)
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "<script>hi</script>"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "safe"})
	dump := m.FormatTree()
	if !strings.Contains(dump, "script") {
		t.Fatalf("tree dump = %s", dump)
	}
	html, err := RenderHTML(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>hi</script>") {
		t.Fatal("html did not escape script tag")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("expected escaped script, got %s", html)
	}
}

func TestUserMessagesForForking(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, dir)
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "alpha"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	got := m.UserMessagesForForking()
	if len(got) != 1 || got[0]["text"] != "alpha" {
		t.Fatalf("%+v", got)
	}
}
