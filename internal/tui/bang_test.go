package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func drainBash(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for cmd != nil && time.Now().Before(deadline) {
		msg := cmd()
		if msg == nil {
			break
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(Model)
		if _, ok := msg.(bashDoneMsg); ok {
			return m
		}
	}
	if m.bashRunning {
		t.Fatal("bash did not finish")
	}
	return m
}

func TestParseBang(t *testing.T) {
	cmd, excl, ok := parseBang("!ls -la")
	if !ok || excl || cmd != "ls -la" {
		t.Fatalf("! got %q excl=%v ok=%v", cmd, excl, ok)
	}
	cmd, excl, ok = parseBang("!!printf secret")
	if !ok || !excl || cmd != "printf secret" {
		t.Fatalf("!! got %q excl=%v ok=%v", cmd, excl, ok)
	}
	if _, _, ok := parseBang("!"); ok {
		t.Fatal("bare ! is not a command")
	}
	if _, _, ok := parseBang("hello"); ok {
		t.Fatal("plain text is not bang")
	}
}

func TestBangPrintfShowsOutput(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("!printf hello")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected bash command")
	}
	m = drainBash(t, m, cmd)
	if m.bashRunning {
		t.Fatal("bash should have finished")
	}
	if len(m.transcript) == 0 || !strings.Contains(m.transcript[0].rendered, "hello") {
		t.Fatalf("transcript=%+v", m.transcript)
	}
	if len(m.history) != 1 || !strings.Contains(m.history[0].Content, "hello") {
		t.Fatalf("history=%+v", m.history)
	}
}

func TestBangShowsLiveOutputWhileRunning(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("!printf live-chunk; sleep 0.4")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected bash command")
	}
	if !m.bashRunning {
		t.Fatal("bash should be running")
	}
	sawLive := false
	deadline := time.Now().Add(3 * time.Second)
	for cmd != nil && time.Now().Before(deadline) {
		msg := cmd()
		if msg == nil {
			break
		}
		var nxt tea.Model
		nxt, cmd = m.Update(msg)
		m = nxt.(Model)
		if _, ok := msg.(bashChunkMsg); ok && m.bashRunning && strings.Contains(m.View(), "live-chunk") {
			sawLive = true
		}
		if _, ok := msg.(bashDoneMsg); ok {
			break
		}
	}
	if !sawLive {
		t.Fatalf("expected live output in view while bash was running; view=\n%s", m.View())
	}
}

func TestBangExcludeSkipsLLMHistory(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("!!printf secret")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainBash(t, next.(Model), cmd)
	if len(m.history) != 0 {
		t.Fatalf("excluded bang should not enter LLM history: %+v", m.history)
	}
	if len(m.transcript) == 0 || !strings.Contains(m.transcript[0].rendered, "secret") {
		t.Fatalf("still shown in transcript: %+v", m.transcript)
	}
}

func TestSlashTabCompletesUniqueCommand(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("/hotk")
	m = send(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.editor.Value() != "/hotkeys " {
		t.Fatalf("got %q", m.editor.Value())
	}
}

func TestSlashTabListsMultipleThenEnter(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("/")
	m = send(m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.complete.active || len(m.complete.items) < 2 {
		t.Fatalf("expected command list, active=%v n=%d", m.complete.active, len(m.complete.items))
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.HasPrefix(m.editor.Value(), "/") {
		t.Fatalf("apply = %q", m.editor.Value())
	}
	if m.complete.active {
		t.Fatal("list should close after apply")
	}
}

func TestFileTabCompletesUniquePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "unique_alpha.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := editorModel()
	m.completeDir = dir
	m.editor.SetValue("@unique")
	m = send(m, tea.KeyMsg{Type: tea.KeyTab})
	if !strings.Contains(m.editor.Value(), "@unique_alpha.go") {
		t.Fatalf("got %q", m.editor.Value())
	}
}

func TestEscHidesCompletionsWithoutApplying(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("/")
	m = send(m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.complete.active {
		t.Fatal("expected list")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.complete.active {
		t.Fatal("esc should close list")
	}
	if m.editor.Value() != "/" {
		t.Fatalf("esc must not apply, got %q", m.editor.Value())
	}
}

func TestBangRunsWhileAgentStreaming(t *testing.T) {
	m := editorModel()
	m.running = true
	m.editor.SetValue("!printf hello")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("bang should run even while the agent is streaming")
	}
	if m.running && len(m.transcript) > 0 && strings.Contains(m.transcript[len(m.transcript)-1].rendered, "steering") {
		t.Fatal("bang must not be treated as steer")
	}
	m = drainBash(t, next.(Model), cmd)
	if len(m.history) != 1 || !strings.Contains(m.history[0].Content, "hello") {
		t.Fatalf("history=%+v", m.history)
	}
}

func TestBareBangIsNotACommand(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("!")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("bare ! falls through to a normal prompt turn")
	}
	if m.bashRunning {
		t.Fatal("bare ! must not start bash")
	}
}

func TestTypingSlashOpensCompletions(t *testing.T) {
	m := editorModel()
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !m.complete.active {
		t.Fatal("typing / should show slash completions")
	}
}

func TestBashModeChangesPrompt(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("!ls")
	if !m.editor.bashMode() || m.editor.ta.Prompt != "$ " {
		t.Fatalf("prompt=%q bash=%v", m.editor.ta.Prompt, m.editor.bashMode())
	}
}
