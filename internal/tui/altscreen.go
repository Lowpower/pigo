package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const altWheelMultiplier = 5

func (m Model) copyLastAssistant() (tea.Model, tea.Cmd) {
	text := lastAssistant(m.history)
	if text == "" {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("no assistant text to copy")})
		return m, nil
	}
	m.clipOSC = osc52(text)
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("copied last assistant message")})
	return m, nil
}

func (m *Model) scrollBy(delta int) {
	if !m.altScreen {
		return
	}
	m.scrollOff += delta
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
}

func (m *Model) scrollTop() {
	m.scrollOff = 1 << 20
}

func (m *Model) scrollBottom() {
	m.scrollOff = 0
}

func (m Model) handleAltScreenKey(msg tea.KeyMsg) (tea.Model, bool) {
	if m.searchActive {
		if m.keyIs(msg, "tui.altScreen.searchClose") || m.keyIs(msg, "app.interrupt") {
			m.searchActive = false
			m.searchQuery = ""
			m.searchHits = nil
			m.searchN = 0
			return m, true
		}
		if m.keyIs(msg, "tui.altScreen.searchNext") {
			m.moveSearch(1)
			return m, true
		}
		if m.keyIs(msg, "tui.altScreen.searchPrevious") {
			m.moveSearch(-1)
			return m, true
		}
		k := msg.String()
		if k == "backspace" {
			if m.searchQuery != "" {
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				m.refreshSearchHits()
			}
			return m, true
		}
		if len(msg.Runes) > 0 && !msg.Alt {
			m.searchQuery += string(msg.Runes)
			m.refreshSearchHits()
			return m, true
		}
		return m, true
	}
	if m.keyIs(msg, "tui.altScreen.search") {
		m.searchActive = true
		m.searchQuery = ""
		m.searchHits = nil
		m.searchN = 0
		return m, true
	}
	page := m.height - 1
	if page < 1 {
		page = 1
	}
	switch {
	case m.keyIs(msg, "tui.altScreen.pageUp"):
		m.scrollBy(page)
		return m, true
	case m.keyIs(msg, "tui.altScreen.pageDown"):
		m.scrollBy(-page)
		return m, true
	case m.keyIs(msg, "tui.altScreen.halfPageUp"):
		m.scrollBy(max(1, page/2))
		return m, true
	case m.keyIs(msg, "tui.altScreen.halfPageDown"):
		m.scrollBy(-max(1, page/2))
		return m, true
	case m.keyIs(msg, "tui.altScreen.lineUp"):
		m.scrollBy(1)
		return m, true
	case m.keyIs(msg, "tui.altScreen.lineDown"):
		m.scrollBy(-1)
		return m, true
	case m.keyIs(msg, "tui.altScreen.previousPrompt"):
		m.scrollBy(max(3, page/4))
		return m, true
	case m.keyIs(msg, "tui.altScreen.nextPrompt"):
		m.scrollBy(-max(3, page/4))
		return m, true
	case m.keyIs(msg, "tui.altScreen.top"):
		m.scrollTop()
		return m, true
	case m.keyIs(msg, "tui.altScreen.bottom"):
		m.scrollBottom()
		return m, true
	}
	return m, false
}

func (m Model) handleAltScreenMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	ev := tea.MouseEvent(msg)
	if ev.IsWheel() {
		n := 1
		if ev.Alt {
			n = altWheelMultiplier
		}
		switch ev.Button {
		case tea.MouseButtonWheelUp:
			m.scrollBy(n)
		case tea.MouseButtonWheelDown:
			m.scrollBy(-n)
		}
		return m, nil
	}
	if ev.Action == tea.MouseActionRelease && ev.Button == tea.MouseButtonLeft && m.scrollOff > 0 {
		if m.height > 0 && ev.Y >= m.height-1 {
			m.scrollBottom()
		}
	}
	return m, nil
}

func (m *Model) refreshSearchHits() {
	q := strings.ToLower(m.searchQuery)
	m.searchHits = nil
	if q == "" {
		m.searchN = 0
		return
	}
	for i, e := range m.transcript {
		text := strings.ToLower(e.rendered + " " + e.thinking + " " + e.toolOut)
		if strings.Contains(text, q) {
			m.searchHits = append(m.searchHits, i)
		}
	}
	if len(m.searchHits) == 0 {
		m.searchN = 0
		return
	}
	m.searchN = 0
	m.jumpSearch()
}

func (m *Model) moveSearch(delta int) {
	if len(m.searchHits) == 0 {
		return
	}
	m.searchN = (m.searchN + delta + len(m.searchHits)) % len(m.searchHits)
	m.jumpSearch()
}

func (m *Model) jumpSearch() {
	if m.searchN < 0 || m.searchN >= len(m.searchHits) {
		return
	}
	hit := m.searchHits[m.searchN]
	remain := len(m.transcript) - hit
	if remain < 1 {
		remain = 1
	}
	m.scrollOff = remain
}
