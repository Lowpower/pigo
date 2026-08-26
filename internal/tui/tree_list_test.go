package tui

import (
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/session"
)

func TestFilterHidesLabelAndToolInDefault(t *testing.T) {
	m := session.New(t.TempDir(), t.TempDir())
	u, _ := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"})
	_, _ = m.AppendMessage("toolResult", map[string]any{"role": "toolResult", "content": "out"})
	_, _ = m.AppendLabel(u.ID, "mark")
	flat := flattenForest(m.GetTree(), m.LeafID())
	def := filterFlat(flat, filterDefault, "", m.LeafID(), nil)
	for _, n := range def {
		if n.node.Entry.Type == "label" {
			t.Fatal("default filter should hide label entries")
		}
	}
	noTools := filterFlat(flat, filterNoTools, "", m.LeafID(), nil)
	for _, n := range noTools {
		if entryRoleOf(n.node.Entry) == "toolResult" {
			t.Fatal("no-tools should hide toolResult")
		}
	}
	users := filterFlat(flat, filterUserOnly, "", m.LeafID(), nil)
	if len(users) != 1 || entryRoleOf(users[0].node.Entry) != "user" {
		t.Fatalf("user-only = %+v", users)
	}
	labeled := filterFlat(flat, filterLabeledOnly, "", m.LeafID(), nil)
	if len(labeled) != 1 || labeled[0].node.Entry.ID != u.ID {
		t.Fatalf("labeled-only = %+v", labeled)
	}
}

func TestSearchAndFold(t *testing.T) {
	m := session.New(t.TempDir(), t.TempDir())
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "alpha"})
	a, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "beta"})
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "gamma"})
	flat := flattenForest(m.GetTree(), m.LeafID())
	got := filterFlat(flat, filterAll, "gamma", m.LeafID(), nil)
	if len(got) != 1 || !strings.Contains(entryDisplay(got[0].node.Entry), "gamma") {
		t.Fatalf("search = %+v", got)
	}
	folded := filterFlat(flat, filterAll, "", m.LeafID(), map[string]bool{a.ID: true})
	for _, n := range folded {
		if strings.Contains(entryDisplay(n.node.Entry), "gamma") {
			t.Fatal("folded parent should hide descendants")
		}
	}
}

func TestRenderUsesConnectorsOnBranch(t *testing.T) {
	m := session.New(t.TempDir(), t.TempDir())
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "root"})
	a, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "left"})
	if err := m.Branch(a.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "right"})
	flat := flattenForest(m.GetTree(), m.LeafID())
	vis := filterFlat(flat, filterAll, "", m.LeafID(), nil)
	var joined strings.Builder
	for i, n := range vis {
		joined.WriteString(renderTreeLine(n, i == 0, n.node.Entry.ID == m.LeafID(), false, false, false, false))
		joined.WriteByte('\n')
	}
	s := joined.String()
	if !strings.Contains(s, "├─") && !strings.Contains(s, "└─") {
		t.Fatalf("expected connectors:\n%s", s)
	}
	if !strings.Contains(s, "• ") {
		t.Fatalf("expected active path marker:\n%s", s)
	}
}

func TestCycleFilter(t *testing.T) {
	if got := cycleFilter(filterDefault, false); got != filterNoTools {
		t.Fatalf("got %s", got)
	}
	if got := cycleFilter(filterDefault, true); got != filterAll {
		t.Fatalf("back %s", got)
	}
}
