package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/models"
)

func (m Model) thinkingPickerActive() bool { return m.thinkingPick.active }

func (m Model) openThinkingPicker() (tea.Model, tea.Cmd) {
	items := make([]pickerItem, 0, len(models.ThinkingLevels))
	cur := strings.ToLower(strings.TrimSpace(m.cfg.Thinking))
	sel := 0
	for i, lvl := range models.ThinkingLevels {
		mark := ""
		if lvl == cur {
			mark = "(current)"
			sel = i
		}
		items = append(items, pickerItem{ID: lvl, Label: lvl, Meta: mark})
	}
	p := listPicker{
		title:        "Select thinking level",
		hint:         "↑↓ select · Enter apply · Ctrl+S default · Esc cancel",
		skipFilter:   true,
		detailPrefix: "Level: ",
	}
	p.setItems(items)
	p.selected = sel
	p.active = true
	m.thinkingPick = p
	return m, nil
}

func (m Model) handleThinkingPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.keyIs(msg, "app.thinking.save") {
		return m.applyThinkingPicker(true)
	}
	act := m.thinkingPick.handleKey(msg, m.keys)
	switch act {
	case "cancel":
		m.thinkingPick = listPicker{}
		return m, nil
	case "confirm":
		return m.applyThinkingPicker(false)
	}
	return m, nil
}

func (m Model) applyThinkingPicker(persist bool) (tea.Model, tea.Cmd) {
	it, ok := m.thinkingPick.current()
	m.thinkingPick = listPicker{}
	if !ok {
		return m, nil
	}
	m.cfg.Thinking = it.ID
	if m.engine != nil {
		if persist {
			_ = m.engine.PersistThinking(it.ID)
			m.cfg = m.engine.Opts.Config
		} else {
			m.engine.Opts.Config.Thinking = it.ID
		}
	} else if persist {
		m.cfg.DefaultThinkingLevel = it.ID
	}
	msg := "thinking = " + it.ID
	if persist {
		msg = "Default thinking: " + it.ID
	}
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(msg)})
	return m, nil
}
