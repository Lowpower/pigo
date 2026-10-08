package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHardwareCursorWhenLastOverlayCloses(t *testing.T) {
	cmd := hardwareCursorCmd(false, true, false)
	if got := cursorCmdKind(cmd); got != "show" {
		t.Fatalf("cursor = %s, want show", got)
	}
}

func TestHardwareCursorCloseAfterQuit(t *testing.T) {
	cmd := hardwareCursorCmd(false, true, true)
	if cmd != nil {
		t.Fatalf("cursor = %s, want none", cursorCmdKind(cmd))
	}
}

func TestHardwareCursorDefaultStaysHidden(t *testing.T) {
	cmd := hardwareCursorCmd(false, false, false)
	if got := cursorCmdKind(cmd); got != "hide" {
		t.Fatalf("cursor = %s, want hide", got)
	}
}

func TestUpdateRestoresCursorWhenLastOverlayCloses(t *testing.T) {
	cfg := testCfg()
	on := true
	cfg.ShowHardwareCursor = &on
	m := New(cfg)
	m.extCustom = &extCustomState{overlay: true}

	next, cmd := m.Update(extCustomOpMsg{op: "custom.close"})
	got := next.(Model)
	if got.overlayVisible() {
		t.Fatal("overlay still visible")
	}
	if kind := cursorCmdKind(cmd); kind != "show" {
		t.Fatalf("cursor = %s, want show", kind)
	}
}

func TestUpdateLeavesCursorAloneWithoutOverlayChange(t *testing.T) {
	m := New(testCfg())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if kind := cursorCmdKind(cmd); kind == "show" || kind == "hide" {
		t.Fatalf("cursor = %s, want no cursor command", kind)
	}
}

func TestUpdateDoesNotHideCursorWhenOverlayClosesAfterQuit(t *testing.T) {
	cfg := testCfg()
	on := true
	cfg.ShowHardwareCursor = &on
	m := New(cfg)
	m.quitting = true
	m.extCustom = &extCustomState{overlay: true}

	_, cmd := m.Update(extCustomOpMsg{op: "custom.close"})
	if kind := cursorCmdKind(cmd); kind != "none" {
		t.Fatalf("cursor = %s, want none", kind)
	}
}

func cursorCmdKind(cmd tea.Cmd) string {
	if cmd == nil {
		return "none"
	}
	return cursorMsgKind(cmd())
}

func cursorMsgKind(msg tea.Msg) string {
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if kind := cursorCmdKind(c); kind == "show" || kind == "hide" {
				return kind
			}
		}
		return "none"
	}
	name := fmt.Sprintf("%T", msg)
	switch {
	case strings.HasSuffix(name, "showCursorMsg"):
		return "show"
	case strings.HasSuffix(name, "hideCursorMsg"):
		return "hide"
	default:
		return "other"
	}
}
