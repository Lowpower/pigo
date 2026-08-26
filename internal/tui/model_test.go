package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/session"
)

func testCfg() config.Config {
	return config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Theme: "default"}
}

func send(m tea.Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestNewlineKeybindingMatchesPi(t *testing.T) {
	keys := New(testCfg()).textarea.KeyMap.InsertNewline.Keys()
	if !slices.Contains(keys, "shift+enter") || !slices.Contains(keys, "ctrl+j") {
		t.Errorf("newline keys = %v, want shift+enter and ctrl+j", keys)
	}
	if slices.Contains(keys, "enter") {
		t.Errorf("Enter must not insert a newline (it sends), keys = %v", keys)
	}
}

func TestCtrlCQuitsWhenIdle(t *testing.T) {
	m := New(testCfg())
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !next.(Model).quitting {
		t.Error("expected quitting after Ctrl+C when idle")
	}
	if cmd == nil {
		t.Error("expected a quit command")
	}
}

func TestSlashQuitExits(t *testing.T) {
	m := New(testCfg())
	m.textarea.SetValue("/quit")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !next.(Model).quitting {
		t.Error("expected quitting after /quit")
	}
}

func TestStreamingThenGlamourFinalize(t *testing.T) {
	m := New(testCfg())

	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageStart, Assistant: &ai.AssistantMessage{}}})
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageUpdate, AIEvent: &ai.Event{Type: ai.EventTextDelta, Delta: "Hello "}}})
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageUpdate, AIEvent: &ai.Event{Type: ai.EventTextDelta, Delta: "world"}}})

	if !m.streamingActive || m.streaming != "Hello world" {
		t.Fatalf("streaming = %q (active=%v), want %q", m.streaming, m.streamingActive, "Hello world")
	}
	if !strings.Contains(m.View(), "Hello world") {
		t.Error("streaming text not shown in view")
	}

	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageEnd, Assistant: &ai.AssistantMessage{
		Content: []*ai.Content{{Type: ai.KindText, Text: "Hello world"}},
	}}})

	if m.streamingActive || m.streaming != "" {
		t.Error("streaming should be cleared after message_end")
	}
	if len(m.transcript) == 0 || m.transcript[len(m.transcript)-1].role != "assistant" {
		t.Fatalf("expected an assistant transcript entry, got %+v", m.transcript)
	}
	if !strings.Contains(m.View(), "Hello world") {
		t.Error("finalized assistant text not shown in view")
	}
	// history carries the assistant turn for the next request
	if len(m.history) == 0 || m.history[len(m.history)-1].Role != ai.RoleAssistant {
		t.Error("assistant message not appended to history")
	}
}

func TestToolEventsRender(t *testing.T) {
	m := New(testCfg())
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolStart, ToolName: "read", Args: map[string]any{"path": "README.md"}}})
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "read", Result: "# pigo\nmore", IsError: false}})

	view := m.View()
	if !strings.Contains(view, "read") || !strings.Contains(view, "# pigo") {
		t.Errorf("tool events not rendered; view=\n%s", view)
	}
}

func TestAgentEndStopsRunning(t *testing.T) {
	m := New(testCfg())
	m.running = true
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventAgentEnd}})
	if m.running {
		t.Error("running should be false after agent_end")
	}
}

func TestCycleThinkingKey(t *testing.T) {
	m := New(testCfg())
	m.cfg.Thinking = "off"
	m = send(m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.cfg.Thinking != "minimal" {
		t.Fatalf("thinking = %s", m.cfg.Thinking)
	}
}

func TestAltEnterQueuesFollowUpWhileRunning(t *testing.T) {
	m := New(testCfg())
	m.running = true
	m.textarea.SetValue("later")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if len(m.queued) != 1 || m.queued[0] != "later" {
		t.Fatalf("queued = %v (engine-less follow-up should use m.queued)", m.queued)
	}
}

func TestCtrlDQuitsWhenEmpty(t *testing.T) {
	m := New(testCfg())
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if !next.(Model).quitting {
		t.Fatal("expected quit")
	}
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
}

func treeModel(t *testing.T) Model {
	t.Helper()
	sess := session.New(t.TempDir(), t.TempDir())
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "hello tree"})
	_, _ = sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Session: sess, AgentDir: t.TempDir(), Cwd: t.TempDir()}}
	return m
}

func TestSlashTreeOpensOverlay(t *testing.T) {
	m := treeModel(t)
	m.textarea.SetValue("/tree")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayTree {
		t.Fatalf("overlay = %d", m.overlay)
	}
	if !strings.Contains(m.View(), "Session Tree") {
		t.Fatalf("view =\n%s", m.View())
	}
	if !strings.Contains(m.View(), "hello tree") {
		t.Fatalf("missing node:\n%s", m.View())
	}
}

func TestTreeCtrlDDoesNotQuit(t *testing.T) {
	m := treeModel(t)
	m.textarea.SetValue("/tree")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	got := next.(Model)
	if got.quitting {
		t.Fatal("ctrl+d in tree must not quit")
	}
	if got.overlay != overlayTree {
		t.Fatalf("overlay = %d", got.overlay)
	}
}

func TestTreeEnterNavigatesUserIntoEditor(t *testing.T) {
	m := treeModel(t)
	m.cfg.BranchSummary.SkipPrompt = boolPtr(true)
	m.textarea.SetValue("/tree")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	// cursor on leaf (assistant). Move up to user.
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayNone {
		t.Fatalf("overlay = %d", m.overlay)
	}
	if !strings.Contains(m.textarea.Value(), "hello tree") {
		t.Fatalf("editor = %q", m.textarea.Value())
	}
}

func TestDoubleEscapeOpensTree(t *testing.T) {
	m := treeModel(t)
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != overlayTree {
		t.Fatalf("overlay = %d", m.overlay)
	}
}

func TestTreeOpensWhileRunning(t *testing.T) {
	m := treeModel(t)
	m.running = true
	m.textarea.SetValue("/tree")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayTree {
		t.Fatalf("overlay = %d", m.overlay)
	}
}

func TestTreeConfirmAbortsThenNavigates(t *testing.T) {
	m := treeModel(t)
	m.cfg.BranchSummary.SkipPrompt = boolPtr(true)
	m.running = true
	cancelled := false
	m.cancel = func() { cancelled = true }
	ch := make(chan agent.Event)
	close(ch)
	m.agentEvents = ch
	m.queued = []string{"later"}
	m.textarea.SetValue("/tree")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !cancelled {
		t.Fatal("expected abort")
	}
	if m.pendingNav == nil {
		t.Fatal("expected pending nav")
	}
	if m.overlay != overlayNone {
		t.Fatalf("overlay = %d", m.overlay)
	}
	if !strings.Contains(m.textarea.Value(), "later") {
		t.Fatalf("queued not restored: %q", m.textarea.Value())
	}
	leafBefore := m.engine.Opts.Session.LeafID()
	m = send(m, agentClosedMsg{})
	if m.pendingNav != nil {
		t.Fatal("pending nav should be cleared")
	}
	if m.engine.Opts.Session.LeafID() == leafBefore {
		t.Fatal("leaf should move after abort completes")
	}
	if strings.TrimSpace(m.textarea.Value()) == "" {
		t.Fatal("restored queue should not be overwritten")
	}
}

func TestCtrlXCopiesTreeSelection(t *testing.T) {
	m := treeModel(t)
	m.textarea.SetValue("/tree")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.clipOSC == "" || !strings.Contains(m.clipOSC, "\x1b]52;c;") {
		t.Fatalf("clipOSC = %q", m.clipOSC)
	}
	if m.tree.status != "Copied selected message to clipboard" {
		t.Fatalf("status = %q", m.tree.status)
	}
	if !strings.Contains(m.View(), "\x1b]52;c;") {
		t.Fatal("view should emit OSC 52")
	}
}

func TestDoubleEscapeForkOpensPicker(t *testing.T) {
	m := treeModel(t)
	m.cfg.DoubleEscapeAction = "fork"
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != overlayFork {
		t.Fatalf("overlay = %d", m.overlay)
	}
	if !strings.Contains(m.View(), "Fork from message") {
		t.Fatalf("view =\n%s", m.View())
	}
}

func TestSlashForkOpensPickerAndConfirms(t *testing.T) {
	m := treeModel(t)
	m.textarea.SetValue("/fork")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayFork {
		t.Fatalf("overlay = %d", m.overlay)
	}
	oldID := m.engine.Opts.Session.ID()
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayNone {
		t.Fatalf("overlay = %d", m.overlay)
	}
	if m.engine.Opts.Session.ID() == oldID {
		t.Fatal("expected new forked session")
	}
	if !strings.Contains(m.textarea.Value(), "hello tree") {
		t.Fatalf("editor = %q", m.textarea.Value())
	}
}

func TestResumePickerSortAndNamedFilter(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	a := session.New(cwd, agent)
	_, _ = a.AppendMessage("user", map[string]any{"role": "user", "content": "alpha chat"})
	_, _ = a.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	a.SetName("Alpha")
	b := session.New(cwd, agent)
	_, _ = b.AppendMessage("user", map[string]any{"role": "user", "content": "beta chat"})
	_, _ = b.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Session: b, AgentDir: agent, Cwd: cwd}}
	next, _ := m.openResumePicker(cwd)
	got := next.(Model)
	if got.resume.sortMode != sortThreaded {
		t.Fatalf("sort = %s", got.resume.sortMode)
	}
	got = send(got, tea.KeyMsg{Type: tea.KeyCtrlS})
	if got.resume.sortMode != sortRecent {
		t.Fatalf("sort after ctrl+s = %s", got.resume.sortMode)
	}
	got = send(got, tea.KeyMsg{Type: tea.KeyCtrlN})
	if got.resume.nameFilter != nameNamed {
		t.Fatalf("nameFilter = %s", got.resume.nameFilter)
	}
	if len(got.resume.rows) != 1 || got.resume.rows[0].session.Name != "Alpha" {
		t.Fatalf("named rows = %+v", got.resume.rows)
	}
	got = send(got, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !strings.Contains(got.resume.query, "n") {
		t.Fatalf("query should accept n, got %q", got.resume.query)
	}
}

func TestSlashResumeOpensPicker(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, agent)
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "old chat"})
	_, _ = sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Session: sess, AgentDir: agent, Cwd: cwd}}
	m.textarea.SetValue("/resume")
	// Summaries uses cwd from os.Getwd, so open picker via helper.
	next, _ := m.openResumePicker(cwd)
	got := next.(Model)
	if got.overlay != overlayResume {
		t.Fatalf("overlay = %d", got.overlay)
	}
	if !strings.Contains(got.View(), "Resume Session") {
		t.Fatalf("view =\n%s", got.View())
	}
}

func boolPtr(v bool) *bool { return &v }
