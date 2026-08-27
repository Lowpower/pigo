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
