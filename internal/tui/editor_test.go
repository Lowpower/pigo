package tui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/keys"
	"github.com/Lowpower/pigo/internal/runtime"
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
	for _, want := range []string{"ctrl+g", paste, "ctrl+y", "ctrl+]", "tab"} {
		if !strings.Contains(text, want) {
			t.Fatalf("hotkeys missing %s:\n%s", want, text)
		}
	}
}

func TestEditorUndoRestoresText(t *testing.T) {
	e := newPromptEditor()
	e.SetValue("hello")
	e.pushUndo()
	e.SetValue("hello!")
	e.undoEdit()
	if e.Value() != "hello" {
		t.Fatalf("undo=%q", e.Value())
	}
}

func TestEditorChromeIsCompact(t *testing.T) {
	m := New(testCfg())
	view := m.View()
	if strings.Contains(view, "Enter send") || strings.Contains(view, "Ctrl+G editor") {
		t.Fatalf("placeholder still dumps keybindings:\n%s", view)
	}
	if !strings.Contains(view, "Ask pigo") {
		t.Fatalf("missing placeholder:\n%s", view)
	}
	if strings.Count(view, "│") > 0 && strings.Contains(view, "│ Ask pigo") {
		t.Fatalf("old left gutter still present:\n%s", view)
	}
	if !strings.Contains(view, "─") {
		t.Fatalf("missing horizontal editor border:\n%s", view)
	}
	inner := m.editor.ta.Height()
	if inner != 1 {
		t.Fatalf("empty editor height=%d, want 1", inner)
	}
}

func TestEditorGrowsWithNewlines(t *testing.T) {
	m := New(testCfg())
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.editor.SetValue("one\ntwo\nthree")
	m.editor.syncHeight(24)
	if m.editor.ta.Height() != 3 {
		t.Fatalf("height=%d, want 3", m.editor.ta.Height())
	}
}

func TestEditorBorderFollowsThinkingAndBash(t *testing.T) {
	m := New(testCfg())
	off := m.editorBorderColor()
	m.cfg.Thinking = "high"
	if m.editorBorderColor() == off && m.theme.Colors == nil {
		// builtin theme falls back to accent vs muted, so they should differ
		if m.editorBorderColor() == m.theme.Muted {
			t.Fatalf("high thinking should not use muted border")
		}
	}
	if m.editorBorderColor() != m.theme.Accent {
		t.Fatalf("high thinking border=%s want accent %s", m.editorBorderColor(), m.theme.Accent)
	}
	m.editor.SetValue("!ls")
	if m.editorBorderColor() != m.theme.Tool {
		t.Fatalf("bash border=%s want tool %s", m.editorBorderColor(), m.theme.Tool)
	}
}

func TestIdleEditorBorderHasNoStatusLabel(t *testing.T) {
	m := New(testCfg())
	m.cfg.Thinking = "high"
	m.width = 80
	got := m.framedEditor()
	for _, leak := range []string{"Compacting", "Retrying", "Summarizing branch"} {
		if strings.Contains(got, leak) {
			t.Fatalf("idle border contains %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "─") {
		t.Fatalf("idle missing dashes:\n%s", got)
	}
	if hasBrailleSpinner(got) {
		t.Fatalf("idle should not spin:\n%s", got)
	}
}

func TestEditorBorderShowsCompactionRetryAndBranch(t *testing.T) {
	m := New(testCfg())
	m.cfg.Thinking = "high"
	m.width = 80

	m = send(m, sessionEventMsg{"type": "compaction_start", "reason": "manual"})
	got := m.framedEditor()
	if !strings.Contains(got, "Compacting context") {
		t.Fatalf("missing compact label:\n%s", got)
	}
	if !hasBrailleSpinner(got) {
		t.Fatalf("missing compact spinner:\n%s", got)
	}
	if !strings.Contains(got, "─") {
		t.Fatalf("compact border dropped dashes:\n%s", got)
	}

	m = send(m, sessionEventMsg{"type": "compaction_end"})
	if strings.Contains(m.framedEditor(), "Compacting") {
		t.Fatalf("compact status stuck:\n%s", m.framedEditor())
	}

	m = send(m, sessionEventMsg{"type": "compaction_start", "reason": "overflow"})
	if !strings.Contains(m.framedEditor(), "Context overflow detected") {
		t.Fatalf("missing overflow label:\n%s", m.framedEditor())
	}
	m = send(m, sessionEventMsg{"type": "compaction_end"})
	m = send(m, sessionEventMsg{"type": "compaction_start", "reason": "threshold"})
	if !strings.Contains(m.framedEditor(), "Auto-compacting") {
		t.Fatalf("missing auto compact label:\n%s", m.framedEditor())
	}
	m = send(m, sessionEventMsg{"type": "compaction_end"})

	m = send(m, sessionEventMsg{
		"type": "auto_retry_start", "attempt": 1, "maxAttempts": 3, "delayMs": 8000,
	})
	got = m.framedEditor()
	if !strings.Contains(got, "Retrying (1/3)") {
		t.Fatalf("missing retry label:\n%s", got)
	}
	if !hasBrailleSpinner(got) {
		t.Fatalf("missing retry spinner:\n%s", got)
	}

	m = send(m, sessionEventMsg{"type": "auto_retry_end"})
	m.border = borderStatus{kind: statusBranch, gen: 1}
	got = m.framedEditor()
	if !strings.Contains(got, "Summarizing branch") {
		t.Fatalf("missing branch label:\n%s", got)
	}
	if !hasBrailleSpinner(got) {
		t.Fatalf("missing branch spinner:\n%s", got)
	}
}

func TestEditorBorderSpinnerOnlyWhenNarrow(t *testing.T) {
	m := New(testCfg())
	m.width = 8
	m = send(m, sessionEventMsg{"type": "compaction_start", "reason": "overflow"})
	got := m.framedEditor()
	if strings.Contains(got, "Compacting") || strings.Contains(got, "Auto-compacting") {
		t.Fatalf("narrow border should drop label:\n%s", got)
	}
	if !hasBrailleSpinner(got) {
		t.Fatalf("narrow border should keep spinner:\n%s", got)
	}
	if visibleWidth(got[:strings.IndexByte(got, '\n')]) > 8 {
		t.Fatalf("top bar wider than layout:\n%s", got)
	}
}

func TestInterruptDuringRetryDoesNotOpenTree(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{}
	m = send(m, sessionEventMsg{
		"type": "auto_retry_start", "attempt": 1, "maxAttempts": 2, "delayMs": 5000,
	})
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != overlayNone {
		t.Fatalf("esc during retry opened overlay %d", m.overlay)
	}
}

func hasBrailleSpinner(s string) bool {
	for _, f := range spinnerFrames {
		if strings.Contains(s, f) {
			return true
		}
	}
	return false
}

func mustDecodePNG() []byte {
	const b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		panic(err)
	}
	return b
}
