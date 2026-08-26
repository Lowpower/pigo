package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNavigateUserMessageMovesLeafToParent(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	u1, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "one"})
	a1, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	u2, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "two"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok2"})

	res, err := m.Navigate(u2.ID, NavigateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.EditorText != "two" {
		t.Fatalf("editor = %q", res.EditorText)
	}
	if m.LeafID() != a1.ID {
		t.Fatalf("leaf = %s want parent %s", m.LeafID(), a1.ID)
	}
	u3, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "alt"})
	if err != nil {
		t.Fatal(err)
	}
	if u3.ParentID == nil || *u3.ParentID != a1.ID {
		t.Fatalf("new branch parent = %v want %s", u3.ParentID, a1.ID)
	}
	path := m.GetBranch("")
	ids := make([]string, len(path))
	for i, e := range path {
		ids[i] = e.ID
	}
	if strings.Join(ids, ",") != strings.Join([]string{u1.ID, a1.ID, u3.ID}, ",") {
		t.Fatalf("branch path = %v", ids)
	}
	_ = u1
}

func TestNavigateRootUserResetsLeaf(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	u1, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "first"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	res, err := m.Navigate(u1.ID, NavigateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.EditorText != "first" || m.LeafID() != "" {
		t.Fatalf("editor=%q leaf=%q", res.EditorText, m.LeafID())
	}
	u2, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "retry"})
	if u2.ParentID != nil {
		t.Fatalf("new root should have null parent, got %v", u2.ParentID)
	}
}

func TestNavigateAssistantKeepsLeafOnTarget(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	a1, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"})
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "later"})
	res, err := m.Navigate(a1.ID, NavigateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.EditorText != "" || m.LeafID() != a1.ID {
		t.Fatalf("editor=%q leaf=%s want %s", res.EditorText, m.LeafID(), a1.ID)
	}
}

func TestAppendLabelResolvedOnTree(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	u, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	a, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"})
	if _, err := m.AppendLabel(u.ID, "bookmark"); err != nil {
		t.Fatal(err)
	}
	tree := m.GetTree()
	if tree[0].Label != "bookmark" {
		t.Fatalf("root label = %q", tree[0].Label)
	}
	if _, err := m.AppendLabel(u.ID, ""); err != nil {
		t.Fatal(err)
	}
	tree = m.GetTree()
	if tree[0].Label != "" {
		t.Fatalf("cleared label = %q", tree[0].Label)
	}
	_ = a
}

func TestNavigateWithSummaryAppendsBranchSummary(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	u1, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "a"})
	a1, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "b"})
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "c"})
	abandoned, ancestor := AbandonedBranch(m, m.LeafID(), a1.ID)
	if ancestor != a1.ID && ancestor != u1.ID {
		t.Fatalf("ancestor = %s", ancestor)
	}
	if len(abandoned) == 0 {
		t.Fatal("expected abandoned entries")
	}
	res, err := m.Navigate(a1.ID, NavigateOpts{Summary: "left that branch"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, ok := m.EntryByID(res.NewLeafID)
	if !ok || leaf.Type != "branch_summary" || leaf.Summary != "left that branch" {
		t.Fatalf("leaf = %+v", leaf)
	}
	if leaf.ParentID == nil || *leaf.ParentID != a1.ID {
		t.Fatalf("summary parent = %v want %s", leaf.ParentID, a1.ID)
	}
}

func TestContextEntriesFollowsLeafNotSiblings(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "main"})
	a, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "side"})
	if _, err := m.Navigate(a.ID, NavigateOpts{}); err != nil {
		t.Fatal(err)
	}
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "alt"})
	ctx := ContextEntries(m)
	texts := make([]string, 0, len(ctx))
	for _, e := range ctx {
		if t := userText(&e); t != "" {
			texts = append(texts, t)
		}
	}
	joined := strings.Join(texts, ",")
	if strings.Contains(joined, "side") {
		t.Fatalf("abandoned branch leaked into context: %s", joined)
	}
	if !strings.Contains(joined, "alt") || !strings.Contains(joined, "main") {
		t.Fatalf("context = %s", joined)
	}
}

func TestSummariesAllListsOtherCwd(t *testing.T) {
	agent := t.TempDir()
	cwd1 := t.TempDir()
	cwd2 := t.TempDir()
	a := New(cwd1, agent)
	_, _ = a.AppendMessage("user", map[string]any{"role": "user", "content": "one"})
	_, _ = a.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	b := New(cwd2, agent)
	_, _ = b.AppendMessage("user", map[string]any{"role": "user", "content": "two"})
	_, _ = b.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	all, err := SummariesAll(agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all = %d %+v", len(all), all)
	}
	local, err := Summaries(cwd1, agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(local) != 1 || local[0].ID != a.ID() {
		t.Fatalf("local = %+v", local)
	}
}

func TestDeleteSessionFile(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, agent)
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "x"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "y"})
	if err := os.Remove(m.File()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.File()); !os.IsNotExist(err) {
		t.Fatal("expected missing file")
	}
	_ = filepath.Dir(m.File())
}
