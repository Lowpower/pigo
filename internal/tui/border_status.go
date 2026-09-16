package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const spinnerInterval = 80 * time.Millisecond

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type borderKind int

const (
	statusIdle borderKind = iota
	statusCompact
	statusRetry
	statusBranch
)

type borderStatus struct {
	kind        borderKind
	reason      string
	attempt     int
	maxAttempts int
	deadline    time.Time
	frame       int
	gen         int
}

type sessionEventMsg map[string]any

type borderTickMsg struct{ gen int }

func eventString(v map[string]any, key string) string {
	s, _ := v[key].(string)
	return s
}

func eventInt(v map[string]any, key string) int {
	switch n := v[key].(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func (m *Model) applySessionEvent(ev sessionEventMsg) {
	switch eventString(ev, "type") {
	case "compaction_start":
		m.setBorder(statusCompact, eventString(ev, "reason"))
	case "compaction_end":
		m.clearBorder(statusCompact)
	case "auto_retry_start", "summarization_retry_scheduled":
		m.setRetryBorder(eventInt(ev, "attempt"), eventInt(ev, "maxAttempts"), eventInt(ev, "delayMs"))
	case "auto_retry_end":
		m.clearBorder(statusRetry)
	case "summarization_retry_attempt_start":
		m.clearBorder(statusRetry)
		if eventString(ev, "source") == "branchSummary" {
			m.setBorder(statusBranch, "")
		} else {
			m.setBorder(statusCompact, eventString(ev, "reason"))
		}
	case "summarization_retry_finished":
		m.clearBorder(statusRetry)
	}
}

func (m *Model) setBorder(kind borderKind, reason string) {
	m.border.kind = kind
	m.border.reason = reason
	m.border.attempt = 0
	m.border.maxAttempts = 0
	m.border.deadline = time.Time{}
	m.border.frame = 0
	m.border.gen++
}

func (m *Model) setRetryBorder(attempt, maxAttempts, delayMs int) {
	m.border.kind = statusRetry
	m.border.reason = ""
	m.border.attempt = attempt
	m.border.maxAttempts = maxAttempts
	m.border.deadline = time.Now().Add(time.Duration(delayMs) * time.Millisecond)
	m.border.frame = 0
	m.border.gen++
}

func (m *Model) clearBorder(kind borderKind) {
	if kind != statusIdle && m.border.kind != kind {
		return
	}
	m.border.kind = statusIdle
	m.border.reason = ""
	m.border.attempt = 0
	m.border.maxAttempts = 0
	m.border.deadline = time.Time{}
	m.border.frame = 0
	m.border.gen++
}

func (m Model) borderTickCmd() tea.Cmd {
	if m.border.kind == statusIdle {
		return nil
	}
	gen := m.border.gen
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg {
		return borderTickMsg{gen: gen}
	})
}

func (m Model) spinnerFrame() string {
	if len(spinnerFrames) == 0 {
		return ""
	}
	i := m.border.frame % len(spinnerFrames)
	if i < 0 {
		i = 0
	}
	return spinnerFrames[i]
}

func (m Model) borderLabel() string {
	cancel := "(Esc to cancel)"
	switch m.border.kind {
	case statusCompact:
		switch m.border.reason {
		case "manual":
			return "Compacting context... " + cancel
		case "overflow":
			return "Context overflow detected, Auto-compacting... " + cancel
		default:
			return "Auto-compacting... " + cancel
		}
	case statusRetry:
		sec := 0
		if !m.border.deadline.IsZero() {
			sec = int(math.Ceil(time.Until(m.border.deadline).Seconds()))
			if sec < 0 {
				sec = 0
			}
		}
		return fmt.Sprintf("Retrying (%d/%d) in %ds... %s", m.border.attempt, m.border.maxAttempts, sec, cancel)
	case statusBranch:
		return "Summarizing branch... " + cancel
	default:
		return ""
	}
}

func (m Model) spinnerColor() string {
	if m.border.kind == statusRetry {
		return m.themeColor("warning", m.theme.Error)
	}
	return m.theme.Accent
}

func (m Model) themeColor(name, fallback string) string {
	if m.theme.Colors != nil {
		if c := strings.TrimSpace(m.theme.Colors[name]); c != "" {
			return c
		}
	}
	return fallback
}

func (m Model) paint(color, text string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
}

func (m Model) renderEditorTopBorder(width int) string {
	border := m.editorBorderColor()
	plain := func(s string) string { return m.paint(border, s) }
	if m.border.kind == statusIdle || width < 1 {
		if width < 1 {
			width = 1
		}
		return plain(strings.Repeat("─", width))
	}

	spin := m.paint(m.spinnerColor(), m.spinnerFrame())
	label := m.borderLabel()
	status := spin
	if label != "" {
		status += " " + m.paint(m.theme.Muted, label)
	}
	statusW := visibleWidth(status)

	if width >= statusW+5 {
		rest := width - statusW - 4
		return plain("── ") + status + plain(" "+strings.Repeat("─", rest))
	}

	spinW := visibleWidth(spin)
	prefix := min(3, max(0, width-spinW))
	suffix := max(0, width-prefix-spinW)
	return plain(strings.Repeat("─", prefix)) + spin + plain(strings.Repeat("─", suffix))
}

func (m *Model) abortBorderStatus() bool {
	switch m.border.kind {
	case statusCompact:
		if m.engine != nil {
			m.engine.AbortCompact()
		}
		return true
	case statusRetry:
		if m.engine != nil {
			m.engine.AbortRetry()
		}
		return true
	case statusBranch:
		if m.summaryCancel != nil {
			m.summaryCancel()
		}
		if m.engine != nil {
			m.engine.AbortRetry()
		}
		return true
	default:
		return false
	}
}
