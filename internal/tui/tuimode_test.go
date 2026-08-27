package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/config"
)

func TestUseAltScreen(t *testing.T) {
	if useAltScreen(config.Config{}) {
		t.Fatal("regular should not use alt screen")
	}
	if !useAltScreen(config.Config{TUIMode: "fullscreen"}) {
		t.Fatal("fullscreen should use alt screen")
	}
}

func TestFullscreenExitText(t *testing.T) {
	m := New(testCfg())
	m.cfg.FullscreenExitOutput = "resume-hint"
	m.transcript = []entry{{role: "assistant", rendered: "secret reply"}}
	if strings.Contains(fullscreenExitText(m), "secret reply") {
		t.Fatal("resume-hint should not dump the transcript")
	}
	if !strings.Contains(fullscreenExitText(m), "pigo") {
		t.Fatalf("hint=%q", fullscreenExitText(m))
	}
	m.cfg.FullscreenExitOutput = "transcript"
	if !strings.Contains(fullscreenExitText(m), "secret reply") {
		t.Fatalf("transcript dump=%q", fullscreenExitText(m))
	}
}

func TestSettingsCyclesTuiMode(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tui-mode")})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)
	if got.cfg.TuiMode() != "fullscreen" {
		t.Fatalf("mode=%s", got.cfg.TuiMode())
	}
	if !got.altScreen {
		t.Fatal("altScreen should be set")
	}
	if cmd == nil {
		t.Fatal("expected EnterAltScreen command")
	}
}
