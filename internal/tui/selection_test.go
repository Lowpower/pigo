package tui

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
)

func TestUseMouseTrackingFollowsFullscreenOnly(t *testing.T) {
	if useMouseTracking(config.Config{}) {
		t.Fatal("regular mode must leave native terminal selection")
	}
	on := true
	if useMouseTracking(config.Config{FullscreenCopyOnSelect: &on}) {
		t.Fatal("copy-on-select must not steal the mouse in regular mode")
	}
	if !useMouseTracking(config.Config{TUIMode: "fullscreen"}) {
		t.Fatal("fullscreen should capture mouse for app-owned selection")
	}
}

func TestExtractScreenTextInclusiveRange(t *testing.T) {
	view := "abc\nHELLO-SELECT\nxyz"
	got := extractScreenText(view, cellPos{X: 0, Y: 1}, cellPos{X: 11, Y: 1})
	if got != "HELLO-SELECT" {
		t.Fatalf("got %q", got)
	}
	got = extractScreenText("\x1b[32mHELLO\x1b[0m", cellPos{X: 1, Y: 0}, cellPos{X: 3, Y: 0})
	if got != "ELL" {
		t.Fatalf("ansi slice=%q", got)
	}
	got = extractScreenText("one\ntwo\nthree", cellPos{X: 1, Y: 0}, cellPos{X: 2, Y: 1})
	if got != "ne\ntwo" {
		t.Fatalf("multiline=%q", got)
	}
}

func TestHighlightSelectionUsesReverseVideo(t *testing.T) {
	view := "HELLO-SELECT"
	got := highlightSelection(view, cellPos{X: 0, Y: 0}, cellPos{X: 4, Y: 0})
	if !strings.Contains(got, "\x1b[7m") {
		t.Fatalf("missing reverse: %q", got)
	}
	if extractScreenText(got, cellPos{X: 0, Y: 0}, cellPos{X: 11, Y: 0}) != "HELLO-SELECT" {
		t.Fatalf("highlight should not change copied text: %q", got)
	}
}

func TestFullscreenDragSelectCopiesOnRelease(t *testing.T) {
	m := New(fullscreenCfg())
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.transcript = []entry{{role: "assistant", rendered: "HELLO-SELECT"}}
	x, y, ok := findPlain(m.View(), "HELLO-SELECT")
	if !ok {
		t.Fatalf("needle missing:\n%s", m.View())
	}
	m = send(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = send(m, tea.MouseMsg{X: x + 11, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = send(m, tea.MouseMsg{X: x + 11, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if got := decodeOSC52(m.clipOSC); got != "HELLO-SELECT" {
		t.Fatalf("copied %q (osc=%q)\nview=\n%s", got, m.clipOSC, m.View())
	}
	if !strings.Contains(m.View(), "\x1b[7m") {
		t.Fatal("selection should stay highlighted after copy-on-select")
	}
}

func TestFullscreenCopyOnSelectFalseUsesCtrlX(t *testing.T) {
	off := false
	cfg := fullscreenCfg()
	cfg.FullscreenCopyOnSelect = &off
	m := New(cfg)
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.transcript = []entry{{role: "assistant", rendered: "HELLO-SELECT"}}
	m.history = []ai.Message{{Role: ai.RoleAssistant, Content: "last-assistant"}}
	x, y, ok := findPlain(m.View(), "HELLO-SELECT")
	if !ok {
		t.Fatalf("needle missing:\n%s", m.View())
	}
	m = send(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = send(m, tea.MouseMsg{X: x + 11, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = send(m, tea.MouseMsg{X: x + 11, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if m.clipOSC != "" {
		t.Fatalf("copy-on-select off should not copy, osc=%q", m.clipOSC)
	}
	if !strings.Contains(m.View(), "\x1b[7m") {
		t.Fatal("selection should remain highlighted")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if got := decodeOSC52(m.clipOSC); got != "HELLO-SELECT" {
		t.Fatalf("ctrl+x should copy selection, got %q", got)
	}
}

func TestAltScreenClickLatestClearsSelection(t *testing.T) {
	m := New(fullscreenCfg())
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	m.scrollOff = 3
	m = send(m, tea.MouseMsg{X: 0, Y: 9, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = send(m, tea.MouseMsg{X: 0, Y: 9, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if m.scrollOff != 0 {
		t.Fatalf("click last row should jump to latest, off=%d", m.scrollOff)
	}
	if m.sel.has() {
		t.Fatal("click should not keep a selection")
	}
}

func TestFullscreenDragSelectCopiesUserAndThinking(t *testing.T) {
	m := New(fullscreenCfg())
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.transcript = []entry{
		{role: "user", rendered: "USER-PROMPT"},
		{role: "thinking", thinking: "THINK-BLOCK", rendered: "THINK-BLOCK"},
	}
	for _, needle := range []string{"USER-PROMPT", "THINK-BLOCK"} {
		x, y, ok := findPlain(m.View(), needle)
		if !ok {
			t.Fatalf("%s missing:\n%s", needle, m.View())
		}
		m.clipOSC = ""
		m = send(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		m = send(m, tea.MouseMsg{X: x + len(needle) - 1, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
		m = send(m, tea.MouseMsg{X: x + len(needle) - 1, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
		if got := decodeOSC52(m.clipOSC); got != needle {
			t.Fatalf("%s copied %q", needle, got)
		}
	}
}

func TestSettingsTogglesFullscreenCopyOnSelect(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	m := New(testCfg())
	m.editor.SetValue("/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("copy on select")})
	if !strings.Contains(m.View(), "Copy on select") {
		t.Fatalf("menu missing copy on select:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "true") {
		t.Fatalf("default should be true:\n%s", m.View())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cfg.CopyOnSelect() {
		t.Fatal("copy on select should toggle off")
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CopyOnSelect() {
		t.Fatal("saved fullscreenCopyOnSelect should be false")
	}
}

func TestSettingsTuiModeTogglesMouseTracking(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tui-mode")})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	types := cmdTypes(t, cmd)
	if !containsType(types, "enterAltScreenMsg") || !containsType(types, "enableMouseCellMotionMsg") {
		t.Fatalf("enter fullscreen cmds=%v", types)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	types = cmdTypes(t, cmd)
	if !containsType(types, "exitAltScreenMsg") || !containsType(types, "disableMouseMsg") {
		t.Fatalf("leave fullscreen cmds=%v", types)
	}
}

func findPlain(view, needle string) (x, y int, ok bool) {
	for i, line := range strings.Split(view, "\n") {
		plain := stripANSI(line)
		if j := strings.Index(plain, needle); j >= 0 {
			return j, i, true
		}
	}
	return 0, 0, false
}

func decodeOSC52(s string) string {
	const prefix = "\x1b]52;c;"
	i := strings.Index(s, prefix)
	if i < 0 {
		return ""
	}
	rest := s[i+len(prefix):]
	end := strings.IndexAny(rest, "\x07\x1b")
	if end >= 0 {
		rest = rest[:end]
	}
	b, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return ""
	}
	return string(b)
}

func cmdTypes(t *testing.T, cmd tea.Cmd) []string {
	t.Helper()
	if cmd == nil {
		return nil
	}
	var out []string
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		switch m := msg.(type) {
		case tea.BatchMsg:
			for _, sub := range m {
				walk(sub)
			}
		default:
			rv := reflect.ValueOf(msg)
			if rv.Kind() == reflect.Slice && rv.Len() > 0 && rv.Type().Elem().Kind() == reflect.Func {
				for i := 0; i < rv.Len(); i++ {
					walk(rv.Index(i).Interface().(tea.Cmd))
				}
				return
			}
			out = append(out, fmt.Sprintf("%T", msg))
		}
	}
	walk(cmd)
	return out
}

func containsType(types []string, name string) bool {
	for _, typ := range types {
		if strings.Contains(typ, name) {
			return true
		}
	}
	return false
}
