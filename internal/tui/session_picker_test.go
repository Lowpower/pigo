package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/session"
)

func TestSlashResumeOpensPicker(t *testing.T) {
	m, sess := resumeFixture(t)
	m.editor.SetValue("/resume")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.sessionPickerActive() {
		t.Fatal("/resume should open the picker")
	}
	view := m.View()
	if !strings.Contains(view, "Resume Session") {
		t.Fatalf("view=\n%s", view)
	}
	if !strings.Contains(view, "hello-resume") && !strings.Contains(view, sess.ID()[:8]) {
		t.Fatalf("missing session row:\n%s", view)
	}
}

func TestSlashResumeIDStillSwitches(t *testing.T) {
	m, sess := resumeFixture(t)
	other := newFlushedSession(t, m.engine.Opts.Cwd, m.engine.Opts.AgentDir, "other")
	m.editor.SetValue("/resume " + other.ID())
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.sessionPickerActive() {
		t.Fatal("id resume should not open picker")
	}
	if m.engine.Opts.Session.ID() != other.ID() {
		t.Fatalf("session = %s want %s (started from %s)", m.engine.Opts.Session.ID(), other.ID(), sess.ID())
	}
}

func TestSessionPickerEnterAdopts(t *testing.T) {
	m, sess := resumeFixture(t)
	other := newFlushedSession(t, m.engine.Opts.Cwd, m.engine.Opts.AgentDir, "pick-me")
	m.editor.SetValue("/resume")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	found := false
	for i, it := range m.sessions.filtered {
		if it.ID == other.File() {
			m.sessions.selected = i
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("other session missing: %+v", m.sessions.filtered)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.sessionPickerActive() {
		t.Fatal("enter should close picker")
	}
	if m.engine.Opts.Session.ID() != other.ID() {
		t.Fatalf("adopted %s want %s (was %s)", m.engine.Opts.Session.ID(), other.ID(), sess.ID())
	}
}

func TestSessionPickerTabLoadsAll(t *testing.T) {
	m, _ := resumeFixture(t)
	otherCwd := t.TempDir()
	_ = newFlushedSession(t, otherCwd, m.engine.Opts.AgentDir, "elsewhere")
	m.editor.SetValue("/resume")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.sessions.scope != sessCurrent {
		t.Fatal("start current")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.sessions.scope != sessAll {
		t.Fatal("tab -> all")
	}
	if len(m.sessions.filtered) < 2 {
		t.Fatalf("all rows = %d", len(m.sessions.filtered))
	}
}

func TestSessionPickerCtrlSCyclesSort(t *testing.T) {
	m, _ := resumeFixture(t)
	m.editor.SetValue("/resume")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.sessions.sort != session.SortThreaded {
		t.Fatal(m.sessions.sort)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.sessions.sort != session.SortRecent {
		t.Fatalf("sort = %s", m.sessions.sort)
	}
}

func TestSessionPickerCtrlNNamedFilter(t *testing.T) {
	m, _ := resumeFixture(t)
	m.engine.Opts.Session.SetName("Alpha")
	_ = newFlushedSession(t, m.engine.Opts.Cwd, m.engine.Opts.AgentDir, "unnamed")
	m.editor.SetValue("/resume")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if m.sessions.names != session.NameNamed {
		t.Fatalf("names = %s", m.sessions.names)
	}
	if len(m.sessions.filtered) != 1 {
		t.Fatalf("named rows = %d (%+v)", len(m.sessions.filtered), m.sessions.filtered)
	}
}

func TestSessionPickerDeleteRefusesCurrent(t *testing.T) {
	m, sess := resumeFixture(t)
	m.editor.SetValue("/resume")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	for i, it := range m.sessions.filtered {
		if it.ID == sess.File() {
			m.sessions.selected = i
			break
		}
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlD})
	if _, err := os.Stat(sess.File()); err != nil {
		t.Fatal("current session was deleted")
	}
	if m.sessions.confirm != "" {
		t.Fatal("should not enter confirm for current session")
	}
}

func TestSessionPickerRenamePersists(t *testing.T) {
	m, sess := resumeFixture(t)
	m.editor.SetValue("/resume")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	for i, it := range m.sessions.filtered {
		if it.ID == sess.File() {
			m.sessions.selected = i
			break
		}
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.sessions.renaming {
		t.Fatal("ctrl+r should enter rename")
	}
	m.sessions.renameBuf = "new-name"
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	h, _, err := session.Load(sess.File())
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "new-name" {
		t.Fatalf("name=%q", h.Name)
	}
}

func TestShouldOpenResumePicker(t *testing.T) {
	if !ShouldOpenResumePicker("interactive", true, "", "", "", false) {
		t.Fatal("interactive --resume")
	}
	if ShouldOpenResumePicker("text", true, "", "", "", false) {
		t.Fatal("print mode stays ContinueRecent")
	}
	if ShouldOpenResumePicker("interactive", true, "abc", "", "", false) {
		t.Fatal("session-id skips picker")
	}
}

func resumeFixture(t *testing.T) (Model, *session.Manager) {
	t.Helper()
	agent := t.TempDir()
	cwd := t.TempDir()
	sess := newFlushedSession(t, cwd, agent, "hello-resume")
	m := New(config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Theme: "default"})
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: agent, Cwd: cwd, Session: sess}}
	return m, sess
}

func newFlushedSession(t *testing.T, cwd, agent, text string) *session.Manager {
	t.Helper()
	m := session.New(cwd, agent)
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": text}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.File()); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSessionDirLayout(t *testing.T) {
	// sanity: two cwds land in different folders under the same agentDir
	agent := t.TempDir()
	a := newFlushedSession(t, t.TempDir(), agent, "a")
	b := newFlushedSession(t, t.TempDir(), agent, "b")
	if filepath.Dir(a.File()) == filepath.Dir(b.File()) {
		t.Fatalf("expected different session dirs: %s vs %s", a.File(), b.File())
	}
}
