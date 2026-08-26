package tui

import (
	"encoding/json"
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
	if len(got) != 1 || !strings.Contains(entryDisplay(got[0].node.Entry, nil), "gamma") {
		t.Fatalf("search = %+v", got)
	}
	folded := filterFlat(flat, filterAll, "", m.LeafID(), map[string]bool{a.ID: true})
	for _, n := range folded {
		if strings.Contains(entryDisplay(n.node.Entry, nil), "gamma") {
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

func TestFormatToolCallReadAndBash(t *testing.T) {
	got := formatToolCall("read", map[string]any{"path": "/tmp/a.go", "offset": 10, "limit": 5})
	if got != "[read: /tmp/a.go:10-14]" {
		t.Fatalf("read = %q", got)
	}
	got = formatToolCall("bash", map[string]any{"command": "echo hi"})
	if got != "[bash: echo hi]" {
		t.Fatalf("bash = %q", got)
	}
}

func TestToolResultUsesAssistantToolCall(t *testing.T) {
	m := session.New(t.TempDir(), t.TempDir())
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "read it"})
	_, _ = m.AppendMessage("assistant", map[string]any{
		"role": "assistant",
		"content": []map[string]any{
			{"type": "toolCall", "id": "c1", "name": "read", "arguments": map[string]any{"path": "a.txt"}},
		},
	})
	_, _ = m.AppendMessage("toolResult", map[string]any{"role": "toolResult", "toolCallId": "c1", "toolName": "read", "content": "hi"})
	flat := flattenForest(m.GetTree(), m.LeafID())
	tools := collectToolCalls(flat)
	var shown string
	for _, n := range flat {
		if entryRoleOf(n.node.Entry) == "toolResult" {
			shown = entryDisplay(n.node.Entry, tools)
		}
	}
	if shown != "[read: a.txt]" {
		t.Fatalf("toolResult display = %q", shown)
	}
}

func TestClipTreeRowsKeepsGutter(t *testing.T) {
	rows := []treeViewRow{
		{gutter: "› ", body: strings.Repeat("x", 80), anchorCol: 70, selected: true},
	}
	out := clipTreeRows(rows, 20)
	if len(out) != 1 {
		t.Fatalf("len=%d", len(out))
	}
	if !strings.HasPrefix(out[0], "› ") {
		t.Fatalf("gutter lost: %q", out[0])
	}
	if len([]rune(out[0])) > 20 {
		t.Fatalf("not clipped: %q", out[0])
	}
	if strings.HasPrefix(strings.TrimPrefix(out[0], "› "), "xxx") && !strings.Contains(out[0], "x") {
		t.Fatal("expected panned body")
	}
}

func TestFoldOrUpJumpsToSegmentStart(t *testing.T) {
	m := session.New(t.TempDir(), t.TempDir())
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "root"})
	a, _ := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "left"})
	if err := m.Branch(a.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "right leaf"})
	flat := flattenForest(m.GetTree(), m.LeafID())
	vis := filterFlat(flat, filterAll, "", m.LeafID(), nil)
	parent, kids := visFamily(vis, flat)
	leaf := len(vis) - 1
	got := findBranchSegmentStart(vis, leaf, "up", parent, kids)
	if got >= leaf {
		t.Fatalf("expected jump up from %d, got %d\n%s", leaf, got, renderAll(vis))
	}
}

func TestEntryCopyTextUser(t *testing.T) {
	e := session.Entry{Type: "message"}
	raw, _ := json.Marshal(map[string]any{"role": "user", "content": "copy me"})
	e.Message = raw
	if got := entryCopyText(e); got != "copy me" {
		t.Fatalf("got %q", got)
	}
}

func renderAll(vis []flatNode) string {
	var b strings.Builder
	for i, n := range vis {
		b.WriteString(renderTreeLine(n, false, false, false, false, false, false))
		b.WriteByte('\n')
		_ = i
	}
	return b.String()
}
