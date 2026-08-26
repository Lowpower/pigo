package tui

import (
	"os"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/keys"
	"github.com/Lowpower/pigo/internal/models"
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
	keys := New(testCfg()).editor.ta.KeyMap.InsertNewline.Keys()
	if !slices.Contains(keys, "shift+enter") || !slices.Contains(keys, "ctrl+j") {
		t.Errorf("newline keys = %v, want shift+enter and ctrl+j", keys)
	}
	if slices.Contains(keys, "enter") {
		t.Errorf("Enter must not insert a newline (it sends), keys = %v", keys)
	}
}

func TestCtrlCClearsEditorWhenIdle(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("hello")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := next.(Model)
	if got.quitting {
		t.Fatal("first Ctrl+C must not quit")
	}
	if cmd != nil {
		t.Fatal("first Ctrl+C must not quit")
	}
	if got.editor.Value() != "" {
		t.Fatalf("editor = %q, want empty", got.editor.Value())
	}
}

func TestCtrlCTwiceQuits(t *testing.T) {
	m := New(testCfg())
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !next.(Model).quitting {
		t.Fatal("second Ctrl+C within 500ms should quit")
	}
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
}

func TestSlashQuitExits(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("/quit")
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
	m.editor.SetValue("later")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if len(m.queued) != 1 || m.queued[0].text != "later" {
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

func TestCtrlLOpensModelPicker(t *testing.T) {
	m := New(testCfg())
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	if !m.modelPickerActive() {
		t.Fatal("ctrl+l should open the model picker")
	}
	view := m.View()
	if !strings.Contains(view, "Select model") {
		t.Fatalf("picker view =\n%s", view)
	}
}

func TestSlashModelOpensPicker(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("/model")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.modelPickerActive() {
		t.Fatal("/model with no args should open the picker")
	}
}

func TestSlashModelExactMatchDoesNotOpenPicker(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("/model anthropic/claude-haiku-4")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.modelPickerActive() {
		t.Fatal("exact spec should apply without a picker")
	}
	if m.cfg.ResolvedModel() != "claude-haiku-4" {
		t.Fatalf("model = %s", m.cfg.ResolvedModel())
	}
	if m.cfg.DefaultModel == "claude-haiku-4" {
		t.Fatal("session-only /model should not rewrite the saved default")
	}
}

func TestModelPickerEnterSelectsSessionOnly(t *testing.T) {
	m := New(config.Config{
		Provider: "anthropic", Model: "claude-sonnet-4",
		DefaultProvider: "anthropic", DefaultModel: "claude-sonnet-4",
		Theme: "default",
	})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	found := false
	for i, it := range m.models.filtered {
		if it.ID == "anthropic/claude-haiku-4" {
			m.models.selected = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing anthropic/claude-haiku-4")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.modelPickerActive() {
		t.Fatal("enter should close the picker")
	}
	if m.cfg.ResolvedModel() != "claude-haiku-4" {
		t.Fatalf("session model = %s", m.cfg.ResolvedModel())
	}
	if m.cfg.DefaultModel != "claude-sonnet-4" {
		t.Fatalf("default = %s, want unchanged", m.cfg.DefaultModel)
	}
}

func TestModelPickerTabTogglesScope(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{Scoped: []models.Spec{
		{Model: models.Model{Provider: "anthropic", ID: "claude-sonnet-4"}},
		{Model: models.Model{Provider: "openai", ID: "gpt-4o"}},
	}}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	if m.models.scope != scopeScoped {
		t.Fatalf("scope = %s, want scoped when --models is set", m.models.scope)
	}
	if len(m.models.filtered) != 2 {
		t.Fatalf("scoped rows = %d", len(m.models.filtered))
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.models.scope != scopeAll {
		t.Fatalf("tab should switch to all, got %s", m.models.scope)
	}
}

func TestModelPickerCtrlSPersistsDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Provider: "anthropic", Model: "claude-sonnet-4",
		DefaultProvider: "anthropic", DefaultModel: "claude-sonnet-4",
		Theme: "default",
	}
	m := New(cfg)
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Config: cfg}}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	found := false
	for i, it := range m.models.filtered {
		if it.ID == "anthropic/claude-haiku-4" {
			m.models.selected = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing anthropic/claude-haiku-4")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.modelPickerActive() {
		t.Fatal("ctrl+s should close the picker")
	}
	if m.cfg.DefaultModel != "claude-haiku-4" {
		t.Fatalf("default = %s", m.cfg.DefaultModel)
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DefaultModel != "claude-haiku-4" {
		t.Fatalf("saved default = %s", loaded.DefaultModel)
	}
}

func TestModelPickerEscapeCancels(t *testing.T) {
	m := New(testCfg())
	orig := m.cfg.ResolvedModel()
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.modelPickerActive() {
		t.Fatal("esc should close the picker")
	}
	if m.cfg.ResolvedModel() != orig {
		t.Fatalf("cancel changed model to %s", m.cfg.ResolvedModel())
	}
}

func TestHotkeysReflectsOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	if err := os.WriteFile(dir+"/keybindings.json", []byte(`{"app.model.select":"ctrl+k"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(testCfg())
	m.keys = keys.NewManager(dir)
	m.editor.SetValue("/hotkeys")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.transcript) == 0 || !strings.Contains(m.transcript[len(m.transcript)-1].rendered, "ctrl+k") {
		t.Fatalf("hotkeys = %+v", m.transcript)
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
	m.editor.SetValue("/tree")
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
	m.editor.SetValue("/tree")
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
	m.editor.SetValue("/tree")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	// cursor on leaf (assistant). Move up to user.
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayNone {
		t.Fatalf("overlay = %d", m.overlay)
	}
	if !strings.Contains(m.editor.Value(), "hello tree") {
		t.Fatalf("editor = %q", m.editor.Value())
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

func boolPtr(v bool) *bool { return &v }
