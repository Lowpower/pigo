package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/ext"
)

type extWidgetNode struct {
	ID    string
	Kind  string
	Props map[string]any
}

type extCustomState struct {
	host     *ext.Host
	id       string
	widgets  []extWidgetNode
	overlay  bool
	anchor   string
	width    int
	margin   int
	focused  bool
	hidden   bool
	focusIdx int
	input    map[string]string
	sel      map[string]int
}

func parseWidgets(v any) []extWidgetNode {
	var raw []any
	switch t := v.(type) {
	case []any:
		raw = t
	case []map[string]any:
		for _, m := range t {
			raw = append(raw, m)
		}
	}
	var out []extWidgetNode
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["kind"].(string)
		switch kind {
		case "text", "markdown", "select", "input", "list", "buttons":
		default:
			continue
		}
		id, _ := m["id"].(string)
		props, _ := m["props"].(map[string]any)
		if props == nil {
			props = map[string]any{}
		}
		out = append(out, extWidgetNode{ID: id, Kind: kind, Props: props})
	}
	return out
}

func overlayOpts(v any) (anchor string, width, margin int) {
	m, _ := v.(map[string]any)
	if m == nil {
		return "center", 0, 1
	}
	anchor, _ = m["anchor"].(string)
	if anchor == "" {
		anchor = "center"
	}
	switch n := m["width"].(type) {
	case float64:
		width = int(n)
	case int:
		width = n
	}
	margin = 1
	if _, ok := m["margin"]; ok {
		switch n := m["margin"].(type) {
		case float64:
			margin = int(n)
		case int:
			margin = n
		}
	}
	return anchor, width, margin
}

func (s *extCustomState) applyOpen(host *ext.Host, args map[string]any) {
	s.host = host
	s.id, _ = args["id"].(string)
	s.widgets = parseWidgets(args["widgets"])
	s.overlay, _ = args["overlay"].(bool)
	s.anchor, s.width, s.margin = overlayOpts(args["overlayOptions"])
	s.focused = true
	s.hidden = false
	s.focusIdx = firstFocusable(s.widgets)
	s.input = map[string]string{}
	s.sel = map[string]int{}
	for _, w := range s.widgets {
		if w.Kind == "input" {
			s.input[w.ID] = strArg(w.Props, "value")
		}
	}
}

func (s *extCustomState) applyUpdate(args map[string]any) {
	if v, ok := args["widgets"]; ok {
		s.widgets = parseWidgets(v)
		s.focusIdx = firstFocusable(s.widgets)
	}
	if v, ok := args["overlayOptions"]; ok {
		s.anchor, s.width, s.margin = overlayOpts(v)
		s.overlay = true
	}
	if v, ok := args["overlay"].(bool); ok {
		s.overlay = v
	}
}

func firstFocusable(widgets []extWidgetNode) int {
	for i, w := range widgets {
		switch w.Kind {
		case "select", "input", "list", "buttons":
			return i
		}
	}
	return 0
}

func (s *extCustomState) current() (extWidgetNode, bool) {
	if s == nil || s.focusIdx < 0 || s.focusIdx >= len(s.widgets) {
		return extWidgetNode{}, false
	}
	return s.widgets[s.focusIdx], true
}

func (s *extCustomState) values() map[string]any {
	out := map[string]any{}
	for _, w := range s.widgets {
		switch w.Kind {
		case "input":
			out[w.ID] = s.input[w.ID]
		case "select", "list", "buttons":
			opts := widgetOptions(w)
			i := s.sel[w.ID]
			if i >= 0 && i < len(opts) {
				out[w.ID] = opts[i]
			}
		}
	}
	return out
}

func widgetOptions(w extWidgetNode) []string {
	switch w.Kind {
	case "select":
		return stringList(w.Props["options"])
	case "list":
		return stringList(w.Props["items"])
	case "buttons":
		out := stringList(w.Props["buttons"])
		if raw, ok := w.Props["buttons"].([]any); ok {
			out = out[:0]
			for _, item := range raw {
				switch t := item.(type) {
				case string:
					out = append(out, t)
				case map[string]any:
					if l, _ := t["label"].(string); l != "" {
						out = append(out, l)
						continue
					}
					out = append(out, fmt.Sprint(t["id"]))
				}
			}
		}
		return out
	}
	return nil
}

func (s *extCustomState) emit(name string, extra map[string]any) {
	if s == nil || s.host == nil {
		return
	}
	args := map[string]any{"id": s.id}
	for k, v := range extra {
		args[k] = v
	}
	go s.host.SendHostEvent(context.Background(), name, args, false)
}

func (m Model) extCustomView() string {
	s := m.extCustom
	if s == nil {
		return ""
	}
	var b strings.Builder
	if s.margin > 0 {
		b.WriteString(strings.Repeat("\n", s.margin))
	}
	for i, w := range s.widgets {
		focus := i == s.focusIdx && s.focused
		b.WriteString(m.renderExtWidget(w, s, focus))
		b.WriteByte('\n')
	}
	b.WriteString(m.footerStyle.Render("Enter submit · Tab next · Esc cancel"))
	return b.String()
}

func (m Model) renderExtWidget(w extWidgetNode, s *extCustomState, focus bool) string {
	mark := "  "
	if focus {
		mark = "→ "
	}
	switch w.Kind {
	case "text":
		return strArg(w.Props, "text")
	case "markdown":
		md := strArg(w.Props, "markdown")
		if md == "" {
			md = strArg(w.Props, "text")
		}
		return m.renderMarkdown(md)
	case "input":
		val := s.input[w.ID]
		ph := strArg(w.Props, "placeholder")
		if val == "" && ph != "" && !focus {
			val = ph
		}
		return mark + "> " + val
	case "select", "list", "buttons":
		opts := widgetOptions(w)
		sel := s.sel[w.ID]
		var b strings.Builder
		if t := strArg(w.Props, "title"); t != "" {
			b.WriteString(m.titleStyle.Render(t))
			b.WriteByte('\n')
		}
		for i, o := range opts {
			itemMark := "  "
			if i == sel {
				itemMark = "• "
			}
			if focus && i == sel {
				itemMark = "→ "
			}
			b.WriteString(itemMark)
			b.WriteString(o)
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n")
	}
	return ""
}

func (m Model) handleExtCustomKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.extCustom
	if s == nil || s.hidden {
		return m, nil
	}
	if m.keyIs(msg, "tui.select.cancel") || m.keyIs(msg, "app.interrupt") {
		s.emit("custom.cancel", map[string]any{})
		m.extCustom = nil
		return m, nil
	}
	if msg.String() == "tab" {
		s.focusIdx = nextFocusable(s.widgets, s.focusIdx)
		return m, nil
	}
	w, ok := s.current()
	if !ok {
		return m, nil
	}
	switch w.Kind {
	case "input":
		if m.keyIs(msg, "tui.input.submit") {
			s.emit("custom.submit", map[string]any{"values": s.values(), "widgetId": w.ID})
			m.extCustom = nil
			return m, nil
		}
		if msg.Type == tea.KeyBackspace || msg.Type == tea.KeyCtrlH {
			cur := s.input[w.ID]
			if cur != "" {
				s.input[w.ID] = cur[:len(cur)-1]
				s.emit("custom.change", map[string]any{"widgetId": w.ID, "value": s.input[w.ID]})
			}
			return m, nil
		}
		if msg.Type == tea.KeyRunes {
			s.input[w.ID] += string(msg.Runes)
			s.emit("custom.change", map[string]any{"widgetId": w.ID, "value": s.input[w.ID]})
			return m, nil
		}
	case "select", "list", "buttons":
		opts := widgetOptions(w)
		if m.keyIs(msg, "tui.select.up") || m.keyIs(msg, "tui.editor.cursorUp") {
			if s.sel[w.ID] > 0 {
				s.sel[w.ID]--
				s.emit("custom.change", map[string]any{"widgetId": w.ID, "value": pickOpt(opts, s.sel[w.ID])})
			}
			return m, nil
		}
		if m.keyIs(msg, "tui.select.down") || m.keyIs(msg, "tui.editor.cursorDown") {
			if s.sel[w.ID]+1 < len(opts) {
				s.sel[w.ID]++
				s.emit("custom.change", map[string]any{"widgetId": w.ID, "value": pickOpt(opts, s.sel[w.ID])})
			}
			return m, nil
		}
		if m.keyIs(msg, "tui.select.confirm") || m.keyIs(msg, "tui.input.submit") {
			s.emit("custom.submit", map[string]any{"values": s.values(), "widgetId": w.ID})
			m.extCustom = nil
			return m, nil
		}
	}
	if m.keyIs(msg, "tui.input.submit") {
		s.emit("custom.submit", map[string]any{"values": s.values()})
		m.extCustom = nil
	}
	return m, nil
}

func pickOpt(opts []string, i int) string {
	if i >= 0 && i < len(opts) {
		return opts[i]
	}
	return ""
}

func nextFocusable(widgets []extWidgetNode, cur int) int {
	if len(widgets) == 0 {
		return 0
	}
	for n := 1; n <= len(widgets); n++ {
		i := (cur + n) % len(widgets)
		switch widgets[i].Kind {
		case "select", "input", "list", "buttons":
			return i
		}
	}
	return cur
}
