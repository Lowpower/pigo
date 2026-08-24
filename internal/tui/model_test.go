package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/config"
)

func testCfg() config.Config {
	return config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Theme: "default"}
}

func TestSubmitAppendsToHistoryAndClears(t *testing.T) {
	m := New(testCfg())
	m.textarea.SetValue("hello world")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(Model)

	if got := nm.History(); len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("history = %v, want [\"hello world\"]", got)
	}
	if nm.textarea.Value() != "" {
		t.Errorf("textarea not cleared after submit: %q", nm.textarea.Value())
	}
}

func TestSubmitIgnoresBlankInput(t *testing.T) {
	m := New(testCfg())
	m.textarea.SetValue("   \n  ")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(Model)

	if len(nm.History()) != 0 {
		t.Fatalf("blank input should not be recorded, got %v", nm.History())
	}
}

func TestSlashQuitExits(t *testing.T) {
	m := New(testCfg())
	m.textarea.SetValue("/quit")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(Model)

	if !nm.quitting {
		t.Error("expected quitting to be true after /quit")
	}
	if !isQuit(cmd) {
		t.Error("expected tea.Quit command after /quit")
	}
}

func TestCtrlCQuits(t *testing.T) {
	m := New(testCfg())
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	nm := next.(Model)

	if !nm.quitting {
		t.Error("expected quitting to be true after Ctrl+C")
	}
	if !isQuit(cmd) {
		t.Error("expected tea.Quit command after Ctrl+C")
	}
}

func TestNewlineKeybindingMatchesPi(t *testing.T) {
	m := New(testCfg())
	keys := m.textarea.KeyMap.InsertNewline.Keys()

	// pi: tui.input.newLine = ["shift+enter", "ctrl+j"]; Enter must NOT insert a newline.
	if !slices.Contains(keys, "shift+enter") || !slices.Contains(keys, "ctrl+j") {
		t.Errorf("newline keys = %v, want to contain shift+enter and ctrl+j", keys)
	}
	if slices.Contains(keys, "enter") {
		t.Errorf("Enter must not be bound to newline (it submits), keys = %v", keys)
	}
}

func TestViewShowsConfigAndFooter(t *testing.T) {
	m := New(testCfg())
	view := m.View()

	for _, want := range []string{"pigo", "provider=anthropic", "model=claude-sonnet-4", "Enter submit"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q\n---\n%s", want, view)
		}
	}
}

// isQuit reports whether cmd is tea.Quit by executing it and inspecting the message.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}
