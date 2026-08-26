package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/keys"
)

func TestListPickerFilterAndWrap(t *testing.T) {
	p := listPicker{active: true}
	p.setItems([]pickerItem{
		{ID: "anthropic/claude-sonnet-4", Label: "claude-sonnet-4", Meta: "[anthropic]"},
		{ID: "openai/gpt-4o", Label: "gpt-4o", Meta: "[openai]"},
		{ID: "google/gemini-2.5-pro", Label: "gemini-2.5-pro", Meta: "[google]"},
	})
	kb := keys.NewManager("")
	p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}, kb)
	p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}, kb)
	it, ok := p.current()
	if !ok || it.ID != "openai/gpt-4o" {
		t.Fatalf("got %+v ok=%v", it, ok)
	}
	p.query = ""
	p.applyFilter()
	p.selected = 0
	if p.handleKey(tea.KeyMsg{Type: tea.KeyUp}, kb) != "move" {
		t.Fatal("up")
	}
	it, _ = p.current()
	if it.ID != "google/gemini-2.5-pro" {
		t.Fatalf("wrap up got %s", it.ID)
	}
}
