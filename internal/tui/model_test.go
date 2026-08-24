package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
)

func testCfg() config.Config {
	return config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Theme: "default"}
}

func send(m tea.Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestNewlineKeybindingMatchesPi(t *testing.T) {
	keys := New(testCfg()).textarea.KeyMap.InsertNewline.Keys()
	if !slices.Contains(keys, "shift+enter") || !slices.Contains(keys, "ctrl+j") {
		t.Errorf("newline keys = %v, want shift+enter and ctrl+j", keys)
	}
	if slices.Contains(keys, "enter") {
		t.Errorf("Enter must not insert a newline (it sends), keys = %v", keys)
	}
}

func TestCtrlCQuitsWhenIdle(t *testing.T) {
	m := New(testCfg())
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !next.(Model).quitting {
		t.Error("expected quitting after Ctrl+C when idle")
	}
	if cmd == nil {
		t.Error("expected a quit command")
	}
}

func TestSlashQuitExits(t *testing.T) {
	m := New(testCfg())
	m.textarea.SetValue("/quit")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !next.(Model).quitting {
		t.Error("expected quitting after /quit")
	}
}

func TestStreamingThenGlamourFinalize(t *testing.T) {
	m := New(testCfg())

	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageStart, Assistant: &ai.AssistantMessage{}}})
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageUpdate, AIEvent: &ai.Event{Type: ai.EventTextDelta, Delta: "Hello "}}})
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageUpdate, AIEvent: &ai.Event{Type: ai.EventTextDelta, Delta: "world"}}})

	if !m.streamingActive || m.streaming != "Hello world" {
		t.Fatalf("streaming = %q (active=%v), want %q", m.streaming, m.streamingActive, "Hello world")
	}
	if !strings.Contains(m.View(), "Hello world") {
		t.Error("streaming text not shown in view")
	}

	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageEnd, Assistant: &ai.AssistantMessage{
		Content: []*ai.Content{{Type: ai.KindText, Text: "Hello world"}},
	}}})

	if m.streamingActive || m.streaming != "" {
		t.Error("streaming should be cleared after message_end")
	}
	if len(m.transcript) == 0 || m.transcript[len(m.transcript)-1].role != "assistant" {
		t.Fatalf("expected an assistant transcript entry, got %+v", m.transcript)
	}
	if !strings.Contains(m.View(), "Hello world") {
		t.Error("finalized assistant text not shown in view")
	}
	// history carries the assistant turn for the next request
	if len(m.history) == 0 || m.history[len(m.history)-1].Role != ai.RoleAssistant {
		t.Error("assistant message not appended to history")
	}
}

func TestToolEventsRender(t *testing.T) {
	m := New(testCfg())
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolStart, ToolName: "read", Args: map[string]any{"path": "README.md"}}})
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "read", Result: "# pigo\nmore", IsError: false}})

	view := m.View()
	if !strings.Contains(view, "read") || !strings.Contains(view, "# pigo") {
		t.Errorf("tool events not rendered; view=\n%s", view)
	}
}

func TestAgentEndStopsRunning(t *testing.T) {
	m := New(testCfg())
	m.running = true
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventAgentEnd}})
	if m.running {
		t.Error("running should be false after agent_end")
	}
}
