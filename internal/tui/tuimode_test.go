package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/config"
)

func fullscreenCfg() config.Config {
	cfg := testCfg()
	cfg.TUIMode = "fullscreen"
	return cfg
}

func viewLineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func viewTail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n > len(lines) {
		n = len(lines)
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

func viewHead(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n > len(lines) {
		n = len(lines)
	}
	return strings.Join(lines[:n], "\n")
}

func assertDockAtBottom(t *testing.T, view string) {
	t.Helper()
	bottom := viewTail(view, 12)
	if !strings.Contains(bottom, "Ask pigo") && !strings.Contains(bottom, "─") {
		t.Fatalf("editor prompt missing from bottom region:\n%s", bottom)
	}
	if !strings.Contains(bottom, "/help") {
		t.Fatalf("footer missing from bottom region:\n%s", bottom)
	}
}

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

func TestFullscreenViewFillsTerminal(t *testing.T) {
	m := New(fullscreenCfg())
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	view := m.View()
	if got := viewLineCount(view); got != 24 {
		t.Fatalf("lines=%d, want 24", got)
	}
	assertDockAtBottom(t, view)
	if !strings.Contains(viewHead(view, 6), "pigo") {
		t.Fatalf("header should stay at the top:\n%s", viewHead(view, 6))
	}
}

func TestFullscreenShortTranscriptStillFills(t *testing.T) {
	m := New(fullscreenCfg())
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	m.transcript = []entry{{role: "meta", rendered: "short"}}
	view := m.View()
	if got := viewLineCount(view); got != 24 {
		t.Fatalf("lines=%d, want 24", got)
	}
	assertDockAtBottom(t, view)
}

func TestFullscreenLongTranscriptClipsAndScrolls(t *testing.T) {
	m := New(fullscreenCfg())
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	for i := 0; i < 40; i++ {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: fmt.Sprintf("TRANSCRIPT-%02d", i)})
	}
	bottom := m.View()
	if got := viewLineCount(bottom); got != 24 {
		t.Fatalf("lines=%d, want 24", got)
	}
	assertDockAtBottom(t, bottom)
	if !strings.Contains(bottom, "TRANSCRIPT-39") {
		t.Fatalf("latest transcript missing at scrollOff=0:\n%s", bottom)
	}

	m.scrollOff = 20
	up := m.View()
	if got := viewLineCount(up); got != 24 {
		t.Fatalf("scrolled lines=%d, want 24", got)
	}
	assertDockAtBottom(t, up)
	if up == bottom {
		t.Fatal("scrollOff should change the visible transcript")
	}
	if strings.Contains(up, "TRANSCRIPT-39") {
		t.Fatalf("latest transcript should scroll off:\n%s", up)
	}
}

func TestRegularViewDoesNotPadToHeight(t *testing.T) {
	m := New(testCfg())
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	got := viewLineCount(m.View())
	if got >= 24 {
		t.Fatalf("regular mode padded to %d lines", got)
	}
}

func TestFullscreenEditorWidthFollowsTerminal(t *testing.T) {
	full := New(fullscreenCfg())
	full = send(full, tea.WindowSizeMsg{Width: 120, Height: 24})
	regular := New(testCfg())
	regular = send(regular, tea.WindowSizeMsg{Width: 120, Height: 24})

	if full.editor.ta.Width() <= regular.editor.ta.Width() {
		t.Fatalf("fullscreen editor width=%d, regular=%d; fullscreen should use the 120-col terminal", full.editor.ta.Width(), regular.editor.ta.Width())
	}
	if full.editor.ta.Width() < 110 {
		t.Fatalf("fullscreen editor width=%d, want ~120 minus prompt/pad", full.editor.ta.Width())
	}
	if regular.editor.ta.Width() > 100 {
		t.Fatalf("regular editor width=%d, should stay capped at 100", regular.editor.ta.Width())
	}
	if full.mermaidWidth() <= regular.mermaidWidth() {
		t.Fatalf("fullscreen markdown wrap=%d, regular=%d", full.mermaidWidth(), regular.mermaidWidth())
	}
}

func TestPresentFillsFullscreenViewport(t *testing.T) {
	m := New(fullscreenCfg())
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	got := m.present("overlay")
	if n := viewLineCount(got); n != 24 {
		t.Fatalf("overlay lines=%d, want 24; view=%q", n, got)
	}
}
