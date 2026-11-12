package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/tools"
)

const shellTimerInterval = time.Second

type shellTimerMsg struct{ gen int }

func isShellTool(name string) bool {
	return name == "bash" || name == "powershell"
}

func (m Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m Model) anyShellLive() bool {
	for _, e := range m.transcript {
		if e.shellLive {
			return true
		}
	}
	return false
}

// armShellTimer starts the shared one-second redraw when a shell call is live
// and a tick is not already in flight.
func (m *Model) armShellTimer() tea.Cmd {
	if m.shellTicking || !m.anyShellLive() {
		return nil
	}
	m.shellTicking = true
	return m.shellTimerCmd()
}

// disarmShellTimer stops the shared tick once no shell call is still running.
func (m *Model) disarmShellTimer() {
	if m.anyShellLive() {
		return
	}
	m.shellTicking = false
	m.shellTimerGen++
}

func (m Model) shellTimerCmd() tea.Cmd {
	gen := m.shellTimerGen
	return tea.Tick(shellTimerInterval, func(time.Time) tea.Msg {
		return shellTimerMsg{gen: gen}
	})
}

func (m Model) writeShellElapsed(write func(string), e entry) {
	if !e.shellLive || !isShellTool(e.toolName) {
		return
	}
	write("\n")
	write(m.metaStyle.Render("Elapsed " + tools.FormatDuration(m.clock().Sub(e.startedAt))))
}
