package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/keys"
)

const pickerMaxVisible = 10

// listPicker is a searchable overlay used by model/session selectors.
type listPicker struct {
	title        string
	hint         string
	query        string
	items        []pickerItem
	filtered     []pickerItem
	selected     int
	active       bool
	detailPrefix string
	skipFilter   bool // when true, handleKey "edit" does not fuzzy-filter items
}

type pickerItem struct {
	ID    string
	Label string
	Meta  string
	Aux   string
}

func (p *listPicker) setItems(items []pickerItem) {
	p.items = items
	p.applyFilter()
}

func (p *listPicker) applyFilter() {
	p.filtered = fuzzyFilter(p.items, p.query, func(it pickerItem) string {
		return it.Label + " " + it.Meta + " " + it.Aux + " " + it.ID
	})
	if p.query != "" {
		p.selected = 0
		return
	}
	if p.selected >= len(p.filtered) {
		p.selected = max(0, len(p.filtered)-1)
	}
}

func (p *listPicker) current() (pickerItem, bool) {
	if p.selected < 0 || p.selected >= len(p.filtered) {
		return pickerItem{}, false
	}
	return p.filtered[p.selected], true
}

func (p *listPicker) move(delta int) {
	n := len(p.filtered)
	if n == 0 {
		return
	}
	p.selected = (p.selected + delta%n + n) % n
}

func (p *listPicker) handleKey(msg tea.KeyMsg, kb *keys.Manager) string {
	k := msg.String()
	if kb.Matches(k, "tui.select.up") {
		p.move(-1)
		return "move"
	}
	if kb.Matches(k, "tui.select.down") {
		p.move(1)
		return "move"
	}
	if kb.Matches(k, "tui.select.confirm") {
		return "confirm"
	}
	if kb.Matches(k, "tui.select.cancel") {
		return "cancel"
	}
	if kb.Matches(k, "tui.input.tab") {
		return "tab"
	}
	switch k {
	case "backspace":
		if p.query != "" {
			p.query = p.query[:len(p.query)-1]
			if !p.skipFilter {
				p.applyFilter()
			}
		}
		return "edit"
	}
	if len(msg.Runes) > 0 && !msg.Alt {
		p.query += string(msg.Runes)
		if !p.skipFilter {
			p.applyFilter()
		}
		return "edit"
	}
	return ""
}

func (p listPicker) view() string {
	var b strings.Builder
	b.WriteString(p.title)
	b.WriteString("\n")
	b.WriteString("> ")
	b.WriteString(p.query)
	b.WriteString("\n\n")
	n := len(p.filtered)
	if n == 0 {
		b.WriteString("  (no matches)\n")
		if p.hint != "" {
			b.WriteString("\n")
			b.WriteString(p.hint)
		}
		return b.String()
	}
	start := max(0, min(p.selected-pickerMaxVisible/2, n-pickerMaxVisible))
	end := min(start+pickerMaxVisible, n)
	for i := start; i < end; i++ {
		it := p.filtered[i]
		mark := "  "
		if i == p.selected {
			mark = "→ "
		}
		b.WriteString(mark)
		b.WriteString(it.Label)
		if it.Meta != "" {
			b.WriteString(" ")
			b.WriteString(it.Meta)
		}
		b.WriteByte('\n')
	}
	if start > 0 || end < n {
		b.WriteString("  (")
		b.WriteString(strconv.Itoa(p.selected + 1))
		b.WriteByte('/')
		b.WriteString(strconv.Itoa(n))
		b.WriteString(")\n")
	}
	if it, ok := p.current(); ok && it.Aux != "" {
		prefix := p.detailPrefix
		if prefix == "" {
			prefix = "Model Name: "
		}
		b.WriteString("\n  ")
		b.WriteString(prefix)
		b.WriteString(it.Aux)
		b.WriteByte('\n')
	}
	if p.hint != "" {
		b.WriteString("\n")
		b.WriteString(p.hint)
	}
	return b.String()
}
