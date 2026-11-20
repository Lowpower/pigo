package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/agent"
)

func TestTwoArgToolExpandsToKeyValueLines(t *testing.T) {
	m := New(testCfg())
	m = send(m, agentEventMsg{agent.Event{
		Type:     agent.EventToolStart,
		ToolName: "widget",
		Args:     map[string]any{"mode": "rw", "path": "a.go"},
	}})
	collapsed := stripANSI(m.View())
	if !strings.Contains(collapsed, `mode="rw" path="a.go"`) {
		t.Fatalf("collapsed args:\n%s", collapsed)
	}
	if strings.Contains(collapsed, "mode: rw") {
		t.Fatalf("collapsed shows expanded args:\n%s", collapsed)
	}

	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	expanded := stripANSI(m.View())
	if !hasTrimmedLine(expanded, "  mode: rw") || !hasTrimmedLine(expanded, "  path: a.go") {
		t.Fatalf("expanded args:\n%s", expanded)
	}
}

func TestCollapsedToolArgsTruncate(t *testing.T) {
	m := New(testCfg())
	m.width = 160
	long := strings.Repeat("a", 200)
	m = send(m, agentEventMsg{agent.Event{
		Type:     agent.EventToolStart,
		ToolName: "widget",
		Args:     map[string]any{"path": long},
	}})
	collapsed := stripANSI(m.View())
	if strings.Contains(collapsed, long) || !strings.Contains(collapsed, "...") {
		t.Fatalf("collapsed args:\n%s", collapsed)
	}
	preview := collapsedArgsPreview(collapsed)
	if runeCount(preview) != 100 {
		t.Fatalf("preview runes = %d, want 100\n%s", runeCount(preview), preview)
	}
}

func TestExpandedToolArgKeepsRawString(t *testing.T) {
	m := New(testCfg())
	m.toolsExpanded = true
	m = send(m, agentEventMsg{agent.Event{
		Type:     agent.EventToolStart,
		ToolName: "widget",
		Args:     map[string]any{"note": "a\tb\r\nc", "n": 2},
	}})
	expanded := stripANSI(m.View())
	if !hasTrimmedLine(expanded, "  n: 2") || !hasTrimmedLine(expanded, "  note: a   b") || !hasTrimmedLine(expanded, "    c") {
		t.Fatalf("expanded args:\n%s", expanded)
	}
}

func hasTrimmedLine(view, want string) bool {
	for _, line := range strings.Split(view, "\n") {
		if strings.TrimRight(line, " ") == want {
			return true
		}
	}
	return false
}

func TestCustomToolRendererSkipsArgLines(t *testing.T) {
	m := New(testCfg())
	m.toolsExpanded = true
	m.transcript = append(m.transcript, entry{
		role:       "tool",
		toolName:   "widget",
		toolArgs:   map[string]any{"mode": "rw", "path": "a.go"},
		toolCustom: true,
		rendered:   "CUSTOM-CALL",
	})
	view := stripANSI(m.View())
	if !strings.Contains(view, "CUSTOM-CALL") || strings.Contains(view, "mode: rw") {
		t.Fatalf("custom renderer:\n%s", view)
	}
}

func TestToolCallLinksPath(t *testing.T) {
	m := New(testCfg())
	m.cfg.Terminal.Hyperlinks = true
	m = send(m, agentEventMsg{agent.Event{
		Type:     agent.EventToolStart,
		ToolName: "read",
		Args:     map[string]any{"path": "/tmp/a.go"},
	}})
	if !strings.Contains(m.View(), "\x1b]8;;file://") || !strings.Contains(m.View(), "/tmp/a.go") {
		t.Fatal("path was not linked")
	}
}

func TestCollapsedLongResultStaysWithinVisualBudget(t *testing.T) {
	m := New(testCfg())
	m.width = 40
	long := strings.Repeat("x", 100_000)
	for _, name := range []string{"bash", "mcp__docs__search"} {
		got := m.toolResultText(name, long, false)
		lines := toolPreviewLines(name)
		width := m.toolPreviewWidth()
		if strings.Contains(got, long) {
			t.Fatalf("%s kept the full result", name)
		}
		total := 0
		for _, line := range strings.Split(got, "\n") {
			w := visibleWidth(line)
			if w > width {
				t.Fatalf("%s line width %d > %d (%q)", name, w, width, line)
			}
			total += w
		}
		if total > width*lines {
			t.Fatalf("%s width %d > budget %d", name, total, width*lines)
		}
		if len(visualLines(got, width)) > lines {
			t.Fatalf("%s visual lines = %d, want <= %d", name, len(visualLines(got, width)), lines)
		}
	}

	m = send(m, agentEventMsg{agent.Event{
		Type: agent.EventToolEnd, ToolName: "bash", Result: long,
	}})
	raw, _ := m.rawChatBody()
	wrapped, _ := wrapWithTitleHits(raw, nil, m.transcriptWidth())
	xLines := 0
	for _, line := range strings.Split(stripANSI(wrapped), "\n") {
		if strings.Count(line, "x") > 10 {
			xLines++
		}
	}
	if xLines > toolPreviewLines("bash") {
		t.Fatalf("wrapped bash preview lines = %d", xLines)
	}
}

func collapsedArgsPreview(view string) string {
	for _, line := range strings.Split(view, "\n") {
		if i := strings.Index(line, "widget "); i >= 0 {
			return line[i+len("widget "):]
		}
	}
	return ""
}

func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
