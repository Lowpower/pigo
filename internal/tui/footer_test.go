package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
)

func TestFormatTokens(t *testing.T) {
	if got := formatTokens(42); got != "42" {
		t.Fatalf("%s", got)
	}
	if got := formatTokens(1500); got != "1.5k" {
		t.Fatalf("%s", got)
	}
	if got := formatTokens(12_000); got != "12k" {
		t.Fatalf("%s", got)
	}
}

func TestFormatCwdForFooter(t *testing.T) {
	if got := formatCwdForFooter("/home/me/src", "/home/me"); got != "~/src" {
		t.Fatalf("%s", got)
	}
	if got := formatCwdForFooter("/home/me", "/home/me"); got != "~" {
		t.Fatalf("%s", got)
	}
	if got := formatCwdForFooter("/tmp", "/home/me"); got != "/tmp" {
		t.Fatalf("%s", got)
	}
}

func TestFooterShowsModelAndThinking(t *testing.T) {
	m := New(testCfg())
	m.cfg.Thinking = "low"
	view := m.View()
	if !strings.Contains(view, "claude-sonnet-4") {
		t.Fatalf("missing model:\n%s", view)
	}
	if !strings.Contains(view, "low") {
		t.Fatalf("missing thinking:\n%s", view)
	}
	if !strings.Contains(view, "Ctrl+T thinking") {
		t.Fatalf("missing hint:\n%s", view)
	}
}

func TestCtrlTHidesThinking(t *testing.T) {
	m := New(testCfg())
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageEnd, Assistant: &ai.AssistantMessage{
		Content: []*ai.Content{{Type: ai.KindThinking, Thinking: "secret chain"}},
	}}})
	if !strings.Contains(m.View(), "secret chain") {
		t.Fatalf("thinking should show:\n%s", m.View())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !m.hideThinking {
		t.Fatal("ctrl+t should hide thinking")
	}
	if strings.Contains(m.View(), "secret chain") {
		t.Fatalf("hidden thinking still visible:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "Thinking…") {
		t.Fatalf("want placeholder:\n%s", m.View())
	}
}

func TestCollapsedToolResultKeepsTook(t *testing.T) {
	raw := "line-one\nline-two\nTook 1m 5s"
	got := toolResultBody(raw, false)
	if !strings.Contains(got, "line-one") || !strings.Contains(got, "Took 1m 5s") {
		t.Fatalf("collapsed = %q", got)
	}
	if strings.Contains(got, "line-two") {
		t.Fatalf("collapsed should hide body lines: %q", got)
	}
	expanded := toolResultBody(raw, true)
	if !strings.Contains(expanded, "line-two") || !strings.Contains(expanded, "Took 1m 5s") {
		t.Fatalf("expanded = %q", expanded)
	}

	m := New(testCfg())
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "bash", Result: "hello-bash\nTook 0.1s"}})
	view := m.View()
	if !strings.Contains(view, "hello-bash") || !strings.Contains(view, "Took 0.1s") {
		t.Fatalf("collapsed view missing duration:\n%s", view)
	}
}

func TestExpandedToolResultKeepsTookPastPreviewCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("row\n")
	}
	b.WriteString("Took 1h 2m 0s")
	got := toolResultBody(b.String(), true)
	if !strings.Contains(got, "Took 1h 2m 0s") {
		t.Fatalf("expanded dropped duration:\n%s", got)
	}
}

func TestReloadedTranscriptKeepsTook(t *testing.T) {
	m := New(testCfg())
	entries := transcriptFromMessages(m, []ai.Message{{
		Role:     ai.RoleToolResult,
		ToolName: "bash",
		Content:  "line-one\nline-two\nTook 1m 5s",
	}})
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	if !strings.Contains(entries[0].rendered, "line-one") || !strings.Contains(entries[0].rendered, "Took 1m 5s") {
		t.Fatalf("reloaded = %q", entries[0].rendered)
	}
	if strings.Contains(entries[0].rendered, "line-two") {
		t.Fatalf("reloaded should stay collapsed: %q", entries[0].rendered)
	}
}

func TestCtrlOExpandsToolOutput(t *testing.T) {
	m := New(testCfg())
	body := "line-one\nline-two\nline-three"
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "read", Result: body}})
	view := m.View()
	if !strings.Contains(view, "line-one") {
		t.Fatalf("collapsed missing first line:\n%s", view)
	}
	if strings.Contains(view, "line-two") {
		t.Fatalf("collapsed should hide extra lines:\n%s", view)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if !m.toolsExpanded {
		t.Fatal("ctrl+o should expand")
	}
	view = m.View()
	if !strings.Contains(view, "line-two") {
		t.Fatalf("expanded missing body:\n%s", view)
	}
}

func TestFooterAccumulatesUsage(t *testing.T) {
	m := New(testCfg())
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageEnd, Assistant: &ai.AssistantMessage{
		Usage:   ai.Usage{Input: 1500, Output: 20, Cost: ai.UsageCost{Total: 0.012}},
		Content: []*ai.Content{{Type: ai.KindText, Text: "ok"}},
	}}})
	view := m.View()
	if !strings.Contains(view, "↑1.5k") {
		t.Fatalf("missing input tokens:\n%s", view)
	}
	if !strings.Contains(view, "$0.012") {
		t.Fatalf("missing cost:\n%s", view)
	}
}

func TestAddUsageKeepsCacheWrite1h(t *testing.T) {
	var dst ai.Usage
	addUsage(&dst, ai.Usage{CacheWrite: 10, CacheWrite1h: 4, Cost: ai.UsageCost{CacheWrite: 1, Total: 1}})
	addUsage(&dst, ai.Usage{CacheWrite: 3, CacheWrite1h: 1, Cost: ai.UsageCost{CacheWrite: 2, Total: 2}})
	if dst.CacheWrite != 13 || dst.CacheWrite1h != 5 {
		t.Fatalf("tokens write=%d 1h=%d", dst.CacheWrite, dst.CacheWrite1h)
	}
	if dst.Cost.CacheWrite != 3 || dst.Cost.Total != 3 {
		t.Fatalf("cost=%+v", dst.Cost)
	}
}
