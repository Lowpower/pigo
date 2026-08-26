package tui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/keys"
)

func pasteImageKey() tea.KeyMsg {
	if keys.UseWindowsKeys() {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true}
	}
	return tea.KeyMsg{Type: tea.KeyCtrlV}
}

func editorModel() Model {
	m := New(testCfg())
	m.keys = keys.NewManager("")
	return m
}

func TestKillRingYankAndAccumulate(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("hello world")
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlW})
	if m.editor.Value() != "hello " {
		t.Fatalf("after ctrl+w: %q", m.editor.Value())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlY})
	if m.editor.Value() != "hello world" {
		t.Fatalf("after yank: %q", m.editor.Value())
	}
	m.editor.SetValue("alpha beta gamma")
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlW})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlW})
	if m.editor.Value() != "alpha " {
		t.Fatalf("accumulated kills: %q", m.editor.Value())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlY})
	if m.editor.Value() != "alpha beta gamma" {
		t.Fatalf("yank accumulated: %q", m.editor.Value())
	}
}

func TestKillRingYankPop(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("one")
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m.editor.SetValue("two")
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m.editor.SetValue("x")
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlY})
	if m.editor.Value() != "xtwo" {
		t.Fatalf("yank latest: %q", m.editor.Value())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Alt: true})
	if m.editor.Value() != "xone" {
		t.Fatalf("yank-pop: %q", m.editor.Value())
	}
}

func TestPromptHistoryUpDown(t *testing.T) {
	m := editorModel()
	m.editor.AddHistory("older")
	m.editor.AddHistory("newer")
	m.editor.Reset()
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.editor.Value() != "newer" {
		t.Fatalf("up: %q", m.editor.Value())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.editor.Value() != "older" {
		t.Fatalf("up again: %q", m.editor.Value())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.editor.Value() != "newer" {
		t.Fatalf("down: %q", m.editor.Value())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.editor.Value() != "" {
		t.Fatalf("down to draft: %q", m.editor.Value())
	}
}

func TestHistoryNotUsedWhenCursorNotAtStart(t *testing.T) {
	m := editorModel()
	m.editor.AddHistory("past")
	m.editor.SetValue("hello")
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.editor.Value() != "hello" {
		t.Fatalf("should stay on current line, got %q", m.editor.Value())
	}
}

func TestJumpToCharForwardAndBack(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("abXcdXef")
	m.editor.moveTo(0, 0)
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	_, col := m.editor.cursorLC()
	if col != 2 {
		t.Fatalf("forward jump col=%d", col)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket, Alt: true})
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	_, col = m.editor.cursorLC()
	if col != 2 {
		t.Fatalf("backward jump with no earlier X should stay, col=%d", col)
	}
	m.editor.moveTo(0, 7)
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket, Alt: true})
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	_, col = m.editor.cursorLC()
	if col != 5 {
		t.Fatalf("backward jump col=%d", col)
	}
}

func TestBracketedPasteFoldsLargeInput(t *testing.T) {
	m := editorModel()
	body := strings.Repeat("line\n", 12)
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(body), Paste: true})
	got := m.editor.Value()
	if !strings.Contains(got, "[paste #1 +13 lines]") && !strings.Contains(got, "[paste #1 +12 lines]") {
		t.Fatalf("expected fold marker, got %q", got)
	}
	if m.editor.Expanded() != strings.TrimRight(body, "\n") && m.editor.Expanded() != body {
		if !strings.HasPrefix(m.editor.Expanded(), "line\n") {
			t.Fatalf("expanded=%q", m.editor.Expanded())
		}
	}
	small := "hello paste"
	m.editor.Reset()
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(small), Paste: true})
	if m.editor.Value() != small {
		t.Fatalf("small paste = %q", m.editor.Value())
	}
}

func TestClipboardPasteImageInsertsPath(t *testing.T) {
	m := editorModel()
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	m.editor.readImage = func() *clipImage {
		return &clipImage{bytes: png, mime: "image/png"}
	}
	m.editor.readText = func() string { return "should-not-use" }
	m = send(m, pasteImageKey())
	got := m.editor.Value()
	if !strings.Contains(got, "pigo-clipboard-") || !strings.HasSuffix(got, ".png") {
		t.Fatalf("image path = %q", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("clipboard file: %v", err)
	}
}

func TestClipboardPasteFallsBackToText(t *testing.T) {
	m := editorModel()
	m.editor.readImage = func() *clipImage { return nil }
	m.editor.readText = func() string { return "clip-text" }
	m = send(m, pasteImageKey())
	if m.editor.Value() != "clip-text" {
		t.Fatalf("got %q", m.editor.Value())
	}
}

func TestSubmitExpandsPasteAndAttachesImages(t *testing.T) {
	m := editorModel()
	dir := t.TempDir()
	png := mustDecodePNG()
	path := filepath.Join(dir, "pigo-clipboard-test.png")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}
	m.editor.SetValue("see " + path)
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.history) != 1 {
		t.Fatalf("history len=%d", len(m.history))
	}
	if len(m.history[0].Images) != 1 {
		t.Fatalf("images=%d", len(m.history[0].Images))
	}
	if m.history[0].Images[0].MimeType != "image/png" {
		t.Fatalf("mime=%s", m.history[0].Images[0].MimeType)
	}
}

func TestSubmitAddsPromptHistory(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("remember me")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.editor.Value() != "" {
		t.Fatal("editor should clear on submit")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.editor.Value() != "remember me" {
		t.Fatalf("history = %q", m.editor.Value())
	}
}

func TestExternalEditorKeyIsBound(t *testing.T) {
	m := editorModel()
	if !m.keyIs(tea.KeyMsg{Type: tea.KeyCtrlG}, "app.editor.external") {
		t.Fatal("ctrl+g should open the external editor")
	}
}

func TestExternalEditorDoneReplacesText(t *testing.T) {
	m := editorModel()
	m.editor.SetValue("old")
	m = send(m, externalEditorDoneMsg{content: "from-editor", ok: true})
	if m.editor.Value() != "from-editor" {
		t.Fatalf("got %q", m.editor.Value())
	}
	m = send(m, externalEditorDoneMsg{content: "ignored", ok: false})
	if m.editor.Value() != "from-editor" {
		t.Fatalf("failed editor changed text to %q", m.editor.Value())
	}
}

func TestHotkeysListsEditorBindings(t *testing.T) {
	text := keys.NewManager("").HotkeysText()
	paste := "ctrl+v"
	if keys.UseWindowsKeys() {
		paste = "alt+v"
	}
	for _, want := range []string{"ctrl+g", paste, "ctrl+y", "ctrl+]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("hotkeys missing %s:\n%s", want, text)
		}
	}
}

func mustDecodePNG() []byte {
	const b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		panic(err)
	}
	return b
}
