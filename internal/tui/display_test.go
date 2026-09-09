package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lowpower/pigo/internal/config"
)

func TestOSC8FileLink(t *testing.T) {
	m := New(testCfg())
	m.cfg.Terminal.Hyperlinks = true
	got := m.linkPath("a.go", "/tmp/a.go")
	if !strings.Contains(got, "\x1b]8;;file://") || !strings.Contains(got, "/tmp/a.go") {
		t.Fatalf("%q", got)
	}
	if !strings.Contains(got, "a.go") {
		t.Fatalf("missing label: %q", got)
	}
}

func TestHyperlinksOffSkipsOSC8(t *testing.T) {
	m := New(testCfg())
	m.cfg.Terminal.Hyperlinks = false
	got := m.linkPath("a.go", "/tmp/a.go")
	if strings.Contains(got, "\x1b]8;;") {
		t.Fatalf("%q", got)
	}
	if got != "a.go" {
		t.Fatalf("%q", got)
	}
}

func TestIndentMarkdownCodeBlocks(t *testing.T) {
	src := "hello\n```\nfoo\n```\n"
	got := indentMarkdownCodeBlocks(src, "\t")
	if !strings.Contains(got, "\tfoo") {
		t.Fatalf("%q", got)
	}
}

func TestFormatToolCallHyperlink(t *testing.T) {
	m := New(testCfg())
	m.cfg.Terminal.Hyperlinks = true
	got := formatToolCall("read", map[string]any{"path": "/tmp/a.go"}, m.linkPath)
	if !strings.Contains(got, "\x1b]8;;file://") || !strings.Contains(got, "/tmp/a.go") {
		t.Fatalf("%q", got)
	}
}

func TestPadLinesAndProgressOSC(t *testing.T) {
	if got := padLines("hi\n", 2); got != "  hi\n" {
		t.Fatalf("pad=%q", got)
	}
	if !strings.Contains(progressOSC(true), "9;4;1") {
		t.Fatal("running progress")
	}
	if !strings.Contains(progressOSC(false), "9;4;0") {
		t.Fatal("idle progress")
	}
	clipped := clipWithScrollbar(strings.Repeat("x\n", 10), 3, 0)
	if strings.Count(clipped, "\n") != 2 {
		t.Fatalf("clipped lines=%q", clipped)
	}
	if !strings.Contains(clipped, "▐") {
		t.Fatalf("missing thumb: %q", clipped)
	}
	up := clipWithScrollbar(strings.Repeat("x\n", 10), 3, 5)
	if !strings.Contains(up, "x") {
		t.Fatalf("offset clip=%q", up)
	}
}

func TestClipWindowPadsShortContent(t *testing.T) {
	got := clipWindow("hi", 5, 0)
	if n := viewLineCount(got); n != 5 {
		t.Fatalf("pad lines=%d, want 5; got=%q", n, got)
	}
	if !strings.HasPrefix(got, "hi") {
		t.Fatalf("short content should stay at the top: %q", got)
	}
	bar := clipWithScrollbar("hi", 4, 0)
	if n := viewLineCount(bar); n != 4 {
		t.Fatalf("scrollbar pad lines=%d, want 4; got=%q", n, bar)
	}
	if strings.Contains(bar, "▐") {
		t.Fatalf("short content should not grow a thumb: %q", bar)
	}
	clipped := clipWindow(strings.Repeat("x\n", 10), 3, 0)
	if n := viewLineCount(clipped); n != 3 {
		t.Fatalf("tall clip lines=%d, want 3", n)
	}
}

func TestOutputPadAppliesToView(t *testing.T) {
	n := 2
	m := New(config.Config{Theme: "default", OutputPad: &n})
	got := m.present("hello")
	if !strings.HasPrefix(got, "  hello") {
		t.Fatalf("%q", got)
	}
}

func TestMarkdownStyleUsesCachedBackground(t *testing.T) {
	if got := markdownStyleFor(true, true); got != "dark" {
		t.Fatalf("tty dark: markdownStyleFor() = %q, want dark", got)
	}
	if got := markdownStyleFor(true, false); got != "light" {
		t.Fatalf("tty light: markdownStyleFor() = %q, want light", got)
	}
	if got := markdownStyleFor(false, true); got != "notty" {
		t.Fatalf("pipe dark: markdownStyleFor() = %q, want notty", got)
	}
	if got := markdownStyleFor(false, false); got != "notty" {
		t.Fatalf("pipe light: markdownStyleFor() = %q, want notty", got)
	}
}

func TestWindowSizeRebuildDoesNotFillEditor(t *testing.T) {
	m := New(testCfg())
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	if m.glam == nil {
		t.Fatal("expected markdown renderer after resize")
	}
	if m.editor.Value() != "" {
		t.Fatalf("resize leaked into editor: %q", m.editor.Value())
	}
}

func TestOSC11LeakKeysAreDropped(t *testing.T) {
	m := New(testCfg())
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]11;rgb:0000/0000/0000\\[1;1R")})
	if m.editor.Value() != "" {
		t.Fatalf("OSC 11 reply leaked into editor: %q", m.editor.Value())
	}
}

func TestWrapTranscriptThinkingFitsWidth(t *testing.T) {
	const width = 80
	cases := []struct {
		name string
		text string
	}{
		{"ascii", strings.Repeat("thinking ", 40)},
		{"cjk", strings.Repeat("思考内容", 40)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(testCfg())
			m.altScreen = true
			m = send(m, tea.WindowSizeMsg{Width: width, Height: 24})
			m.streamingThinking = tc.text
			view := m.View()
			for _, line := range strings.Split(view, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Fatalf("visible width %d > %d: %q", w, width, line)
				}
			}
			probe := "thinking"
			if tc.name == "cjk" {
				probe = "思考"
			}
			if !strings.Contains(view, probe) {
				t.Fatalf("thinking text dropped:\n%s", view)
			}
		})
	}
}

func TestFirstLineDoesNotSplitUTF8(t *testing.T) {
	got := firstLine(strings.Repeat("测", 120))
	if !utf8.ValidString(got) {
		t.Fatalf("invalid utf8: %q", got)
	}
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("replacement rune: %q", got)
	}
	if !strings.Contains(got, "测") {
		t.Fatalf("CJK dropped: %q", got)
	}
}

func TestHideThinkingFromSettings(t *testing.T) {
	on := true
	m := New(config.Config{HideThinkingBlock: &on, Theme: "default"})
	if !m.hideThinking {
		t.Fatal("should start hidden")
	}
}
