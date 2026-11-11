package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/agent"
)

func TestShellElapsedAdvancesThenUsesRecordedTook(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	now := start
	m := New(testCfg())
	m.now = func() time.Time { return now }

	m = send(m, agentEventMsg{agent.Event{
		Type:       agent.EventToolStart,
		ToolCallID: "c1",
		ToolName:   "bash",
		Args:       map[string]any{"command": "sleep 30"},
	}})
	if !m.shellTicking {
		t.Fatal("bash start did not arm the timer")
	}
	if !strings.Contains(m.View(), "Elapsed 0.0s") {
		t.Fatalf("missing initial elapsed:\n%s", m.View())
	}
	if strings.Contains(m.transcript[0].toolOut, "Elapsed") {
		t.Fatal("elapsed leaked into tool output")
	}

	now = start.Add(2 * time.Second)
	view := m.View()
	if !strings.Contains(view, "Elapsed 2.0s") || strings.Contains(view, "Elapsed 0.0s") {
		t.Fatalf("elapsed did not advance:\n%s", view)
	}

	now = start.Add(5 * time.Second)
	m = send(m, agentEventMsg{agent.Event{
		Type:       agent.EventToolUpdate,
		ToolCallID: "c1",
		ToolName:   "bash",
		Result:     "hello-stream",
	}})
	if strings.Contains(m.transcript[0].toolOut, "Elapsed") || strings.Contains(m.transcript[0].toolOut, "Took") {
		t.Fatalf("partial output = %q", m.transcript[0].toolOut)
	}
	view = m.View()
	if !strings.Contains(view, "hello-stream") || !strings.Contains(view, "Elapsed 5.0s") {
		t.Fatalf("partial view:\n%s", view)
	}

	now = start.Add(30 * time.Second)
	m = send(m, agentEventMsg{agent.Event{
		Type:       agent.EventToolEnd,
		ToolCallID: "c1",
		ToolName:   "bash",
		Result:     "hello-stream\nTook 0.1s",
	}})
	view = m.View()
	if !strings.Contains(view, "Took 0.1s") {
		t.Fatalf("missing recorded duration:\n%s", view)
	}
	if strings.Contains(view, "Elapsed") || strings.Contains(view, "30.0s") || strings.Contains(view, "5.0s") {
		t.Fatalf("wall clock replaced recorded duration:\n%s", view)
	}
	if m.shellTicking {
		t.Fatal("timer still armed after the shell finished")
	}
}

func TestReadToolHasNoElapsed(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m := New(testCfg())
	m.now = func() time.Time { return now }
	m = send(m, agentEventMsg{agent.Event{
		Type:       agent.EventToolStart,
		ToolCallID: "r1",
		ToolName:   "read",
		Args:       map[string]any{"path": "README.md"},
	}})
	now = now.Add(3 * time.Second)
	if m.shellTicking || strings.Contains(m.View(), "Elapsed") {
		t.Fatalf("read showed a timer:\n%s", m.View())
	}
}

func TestShellTimersAreIndependentUntilTheLastEnds(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	now := start
	m := New(testCfg())
	m.now = func() time.Time { return now }

	m = send(m, agentEventMsg{agent.Event{
		Type:       agent.EventToolStart,
		ToolCallID: "b1",
		ToolName:   "bash",
		Args:       map[string]any{"command": "sleep 5"},
	}})
	gen := m.shellTimerGen
	now = start.Add(2 * time.Second)
	m = send(m, agentEventMsg{agent.Event{
		Type:       agent.EventToolStart,
		ToolCallID: "p1",
		ToolName:   "powershell",
		Args:       map[string]any{"command": "Start-Sleep 5"},
	}})
	if m.shellTimerGen != gen {
		t.Fatalf("second shell started a new timer generation: %d", m.shellTimerGen)
	}
	view := m.View()
	if !strings.Contains(view, "Elapsed 2.0s") || !strings.Contains(view, "Elapsed 0.0s") {
		t.Fatalf("shells do not keep their own clocks:\n%s", view)
	}

	m = send(m, agentEventMsg{agent.Event{
		Type:       agent.EventToolEnd,
		ToolCallID: "b1",
		ToolName:   "bash",
		Result:     "ok\nTook 2.0s",
	}})
	if !m.shellTicking {
		t.Fatal("timer stopped while powershell was still running")
	}
	now = start.Add(3 * time.Second)
	view = m.View()
	if strings.Count(view, "Elapsed") != 1 || !strings.Contains(view, "Elapsed 1.0s") || !strings.Contains(view, "Took 2.0s") {
		t.Fatalf("after bash ended:\n%s", view)
	}

	m = send(m, agentEventMsg{agent.Event{
		Type:       agent.EventToolEnd,
		ToolCallID: "p1",
		ToolName:   "powershell",
		Result:     "ok\nTook 0.4s",
	}})
	view = m.View()
	if m.shellTicking || strings.Contains(view, "Elapsed") {
		t.Fatalf("timer kept running:\n%s", view)
	}
	if !strings.Contains(view, "Took 2.0s") || !strings.Contains(view, "Took 0.4s") {
		t.Fatalf("recorded durations:\n%s", view)
	}

	m = send(m, shellTimerMsg{gen: gen})
	if m.shellTicking {
		t.Fatal("stale tick revived the timer")
	}
}

func TestLiveShellTickReschedules(t *testing.T) {
	m := New(testCfg())
	m = send(m, agentEventMsg{agent.Event{
		Type:       agent.EventToolStart,
		ToolCallID: "c1",
		ToolName:   "bash",
		Args:       map[string]any{"command": "true"},
	}})
	next, cmd := m.Update(shellTimerMsg{gen: m.shellTimerGen})
	if cmd == nil || !next.(Model).shellTicking {
		t.Fatal("live tick did not reschedule")
	}
}
