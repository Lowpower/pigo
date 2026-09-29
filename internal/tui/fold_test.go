package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/skills"
)

func TestFoldUserEntryKinds(t *testing.T) {
	xml, ok := skills.ExpandCommand([]skills.Skill{{
		Name: "demo", FilePath: "/tmp/demo/SKILL.md", Body: "SKILL-BODY",
	}}, "demo", "")
	if !ok {
		t.Fatal("expand")
	}
	fe, ok := foldUserEntry(xml)
	if !ok || fe.foldKind != "skill" || fe.foldTitle != "[skill] demo" || fe.foldBody != "SKILL-BODY" {
		t.Fatalf("skill entry=%+v ok=%v", fe, ok)
	}
	comp := compaction.SummaryPrefix + "COMPACT-BODY" + compaction.SummarySuffix
	fe, ok = foldUserEntry(comp)
	if !ok || fe.foldKind != "compaction" || fe.foldTitle != "[compaction]" || fe.foldBody != "COMPACT-BODY" {
		t.Fatalf("compaction entry=%+v ok=%v", fe, ok)
	}
	br := compaction.BranchSummaryPrefix + "BRANCH-BODY" + compaction.BranchSummarySuffix
	fe, ok = foldUserEntry(br)
	if !ok || fe.foldKind != "branch" || fe.foldTitle != "[branch]" || fe.foldBody != "BRANCH-BODY" {
		t.Fatalf("branch entry=%+v ok=%v", fe, ok)
	}
	if _, ok := foldUserEntry("plain question"); ok {
		t.Fatal("plain text is not a fold block")
	}
}

func TestClickTitleTogglesFold(t *testing.T) {
	m := foldModel(t, "SKILL-BODY-TEXT")
	if strings.Contains(m.View(), "SKILL-BODY-TEXT") {
		t.Fatal("collapsed view should hide the body")
	}
	x, y, ok := findPlain(m.View(), "[skill] demo")
	if !ok {
		t.Fatalf("title missing:\n%s", m.View())
	}
	id := m.transcript[0].foldID
	m = clickAt(m, x, y)
	if !m.blockOpen[id] || !strings.Contains(m.View(), "SKILL-BODY-TEXT") {
		t.Fatalf("expand open=%v\n%s", m.blockOpen[id], m.View())
	}
	m = clickAt(m, x, y)
	if m.blockOpen[id] || strings.Contains(m.View(), "SKILL-BODY-TEXT") {
		t.Fatal("second click should collapse")
	}
}

func TestClickTitleDoesNotWriteSession(t *testing.T) {
	dir := t.TempDir()
	sess := session.New(dir, dir)
	xml, ok := skills.ExpandCommand([]skills.Skill{{
		Name: "demo", FilePath: "/tmp/demo/SKILL.md", Body: "SKILL-BODY-TEXT",
	}}, "demo", "")
	if !ok {
		t.Fatal("expand")
	}
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": xml}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(sess.File())
	if err != nil {
		t.Fatal(err)
	}
	n := len(sess.Entries())
	m := New(fullscreenCfg())
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.engine = &runtime.Engine{Opts: runtime.Options{Session: sess}}
	m.transcript = transcriptFromMessages(m, session.RestoreAIMessages(session.ContextEntries(sess)))
	x, y, ok := findPlain(m.View(), "[skill] demo")
	if !ok {
		t.Fatalf("title missing:\n%s", m.View())
	}
	m = clickAt(m, x, y)
	after, err := os.ReadFile(sess.File())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || len(sess.Entries()) != n {
		t.Fatalf("session changed entries %d -> %d", n, len(sess.Entries()))
	}
	if !m.blockOpen[m.transcript[0].foldID] {
		t.Fatal("view fold should still open")
	}
}

func TestDragOnTitleDoesNotToggle(t *testing.T) {
	m := foldModel(t, "SKILL-BODY-TEXT")
	x, y, ok := findPlain(m.View(), "[skill] demo")
	if !ok {
		t.Fatal("title missing")
	}
	m = send(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = send(m, tea.MouseMsg{X: x + 4, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = send(m, tea.MouseMsg{X: x + 4, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if m.blockOpen[m.transcript[0].foldID] {
		t.Fatal("drag should not expand")
	}
	if !m.sel.has() {
		t.Fatal("drag should keep a selection")
	}
	if strings.Contains(m.View(), "SKILL-BODY-TEXT") {
		t.Fatal("body should stay hidden")
	}
}

func TestKeysDoNotToggleFold(t *testing.T) {
	m := foldModel(t, "SKILL-BODY-TEXT")
	id := m.transcript[0].foldID
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if strings.Contains(m.View(), "SKILL-BODY-TEXT") {
		t.Fatal("body should stay hidden after keys")
	}
	m.overlay = overlayTree
	m = send(m, tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	if m.blockOpen[id] {
		t.Fatal("keyboard path should not open a fold")
	}
}

func TestClickLastRowStillJumpsWhenNotTitle(t *testing.T) {
	m := foldModel(t, "SKILL-BODY-TEXT")
	m = send(m, tea.WindowSizeMsg{Width: 40, Height: 10})
	m.scrollOff = 3
	if id := m.titleAt(m.height - 1); id != "" {
		t.Fatalf("jump row should not be a title, got %s", id)
	}
	m = clickAt(m, 0, m.height-1)
	if m.scrollOff != 0 {
		t.Fatalf("last row should jump to latest, off=%d", m.scrollOff)
	}
	if m.blockOpen[m.transcript[0].foldID] {
		t.Fatal("jump click should not toggle the title")
	}
}

func TestReloadKeepsFoldState(t *testing.T) {
	m := foldModel(t, "SKILL-BODY-TEXT")
	id := m.transcript[0].foldID
	m.blockOpen = map[string]bool{id: true}
	m.history = []ai.Message{{Role: ai.RoleUser, Content: m.transcript[0].rendered}}
	m.transcript = transcriptFromMessages(m, m.history)
	if m.transcript[0].foldID != id {
		t.Fatalf("id changed %s -> %s", id, m.transcript[0].foldID)
	}
	if !strings.Contains(m.View(), "SKILL-BODY-TEXT") {
		t.Fatal("reload should keep the block expanded")
	}
}

func foldModel(t *testing.T, body string) Model {
	t.Helper()
	xml, ok := skills.ExpandCommand([]skills.Skill{{
		Name: "demo", FilePath: "/tmp/demo/SKILL.md", Body: body,
	}}, "demo", "")
	if !ok {
		t.Fatal("expand")
	}
	fe, ok := foldUserEntry(xml)
	if !ok {
		t.Fatal("fold")
	}
	m := New(fullscreenCfg())
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.transcript = []entry{fe}
	return m
}

func clickAt(m Model, x, y int) Model {
	m = send(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return send(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
}
