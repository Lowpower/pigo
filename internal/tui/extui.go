package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/keys"
)

type extUIHub struct {
	send func(tea.Msg)
}

type extNotifyMsg struct{ level, text string }
type extStatusMsg struct{ key, text string }
type extWidgetMsg struct {
	key, placement string
	lines          []string
}
type extTitleMsg struct{ title string }
type extUIReqMsg struct {
	method string
	args   map[string]any
	reply  chan map[string]any
}

type extWidget struct {
	key, placement string
	lines          []string
}

type extUIState struct {
	active  bool
	method  string
	title   string
	options []string
	sel     int
	input   string
	reply   chan map[string]any
}

func (m *Model) attachExtensions() {
	if m.engine == nil {
		return
	}
	if m.extHub == nil {
		m.extHub = &extUIHub{}
	}
	hub := m.extHub
	if m.keys != nil {
		m.keys.ClearExtensions()
		for _, s := range m.engine.Hosts {
			if s == nil {
				continue
			}
			for _, sc := range s.Shortcuts() {
				m.keys.BindExtension(sc.Name, sc.Description)
			}
		}
	}
	ui := func(method string, args map[string]any, timeout time.Duration) map[string]any {
		if hub.send == nil {
			return map[string]any{"cancelled": true}
		}
		switch method {
		case "setWidget":
			hub.send(widgetMsgFromArgs(args))
			return map[string]any{}
		case "setTitle":
			title, _ := args["title"].(string)
			if title == "" {
				title, _ = args["text"].(string)
			}
			hub.send(extTitleMsg{title: title})
			return map[string]any{}
		}
		if method != "select" && method != "confirm" && method != "input" {
			return map[string]any{"cancelled": true}
		}
		reply := make(chan map[string]any, 1)
		hub.send(extUIReqMsg{method: method, args: args, reply: reply})
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		select {
		case r := <-reply:
			if r == nil {
				return map[string]any{"cancelled": true}
			}
			return r
		case <-time.After(timeout):
			return map[string]any{"cancelled": true}
		}
	}
	for _, h := range m.engine.Hosts {
		if h == nil {
			continue
		}
		h.SetUI(ui)
		h.SetNotify(func(level, text string) {
			if hub.send != nil {
				hub.send(extNotifyMsg{level: level, text: text})
			}
		})
		h.SetStatus(func(key, text string) {
			if hub.send != nil {
				hub.send(extStatusMsg{key: key, text: text})
			}
		})
	}
}

func widgetMsgFromArgs(args map[string]any) extWidgetMsg {
	key, _ := args["key"].(string)
	placement, _ := args["placement"].(string)
	var lines []string
	switch v := args["lines"].(type) {
	case []any:
		for _, item := range v {
			lines = append(lines, fmt.Sprint(item))
		}
	case []string:
		lines = v
	}
	return extWidgetMsg{key: key, placement: placement, lines: lines}
}

func (m Model) handleExtMsg(msg tea.Msg) (Model, bool) {
	switch x := msg.(type) {
	case extNotifyMsg:
		text := x.text
		if x.level != "" && x.level != "info" {
			text = x.level + ": " + text
		}
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(text)})
		return m, true
	case extStatusMsg:
		if m.extStatus == nil {
			m.extStatus = map[string]string{}
		}
		if strings.TrimSpace(x.text) == "" {
			delete(m.extStatus, x.key)
		} else {
			m.extStatus[x.key] = x.text
		}
		return m, true
	case extWidgetMsg:
		m.setExtWidget(x)
		return m, true
	case extTitleMsg:
		m.extTitle = x.title
		return m, true
	case extUIReqMsg:
		if m.extUI.active {
			if x.reply != nil {
				x.reply <- map[string]any{"cancelled": true}
			}
			return m, true
		}
		m.extUI = extUIState{active: true, method: x.method, reply: x.reply, title: strArg(x.args, "title")}
		if m.extUI.title == "" {
			m.extUI.title = strArg(x.args, "message")
		}
		switch x.method {
		case "select":
			m.extUI.options = stringList(x.args["options"])
		case "confirm":
			m.extUI.options = []string{"Yes", "No"}
		}
		return m, true
	}
	return m, false
}

func (m *Model) setExtWidget(msg extWidgetMsg) {
	if msg.key == "" {
		return
	}
	filtered := m.extWidgets[:0]
	for _, w := range m.extWidgets {
		if w.key != msg.key {
			filtered = append(filtered, w)
		}
	}
	m.extWidgets = filtered
	if len(msg.lines) == 0 {
		return
	}
	place := msg.placement
	if place == "" {
		place = "aboveEditor"
	}
	m.extWidgets = append(m.extWidgets, extWidget{key: msg.key, placement: place, lines: msg.lines})
}

func (m Model) widgets(placement string) string {
	var b strings.Builder
	for _, w := range m.extWidgets {
		if w.placement != placement {
			continue
		}
		b.WriteString(strings.Join(w.lines, "\n"))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m Model) handleExtUIKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.keyIs(msg, "tui.select.cancel") || m.keyIs(msg, "app.interrupt") {
		m.finishExtUI(map[string]any{"cancelled": true})
		return m, nil
	}
	switch m.extUI.method {
	case "select", "confirm":
		if m.keyIs(msg, "tui.select.up") || m.keyIs(msg, "tui.editor.cursorUp") {
			if m.extUI.sel > 0 {
				m.extUI.sel--
			}
			return m, nil
		}
		if m.keyIs(msg, "tui.select.down") || m.keyIs(msg, "tui.editor.cursorDown") {
			if m.extUI.sel+1 < len(m.extUI.options) {
				m.extUI.sel++
			}
			return m, nil
		}
		if m.keyIs(msg, "tui.select.confirm") || m.keyIs(msg, "tui.input.submit") {
			if m.extUI.method == "confirm" {
				m.finishExtUI(map[string]any{"confirmed": m.extUI.sel == 0})
				return m, nil
			}
			if m.extUI.sel >= 0 && m.extUI.sel < len(m.extUI.options) {
				m.finishExtUI(map[string]any{"index": m.extUI.sel, "value": m.extUI.options[m.extUI.sel]})
			} else {
				m.finishExtUI(map[string]any{"cancelled": true})
			}
			return m, nil
		}
	case "input":
		if m.keyIs(msg, "tui.input.submit") {
			m.finishExtUI(map[string]any{"value": m.extUI.input})
			return m, nil
		}
		if msg.Type == tea.KeyBackspace || msg.Type == tea.KeyCtrlH {
			if m.extUI.input != "" {
				m.extUI.input = m.extUI.input[:len(m.extUI.input)-1]
			}
			return m, nil
		}
		if msg.Type == tea.KeyRunes {
			m.extUI.input += string(msg.Runes)
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) finishExtUI(result map[string]any) {
	if m.extUI.reply != nil {
		m.extUI.reply <- result
	}
	m.extUI = extUIState{}
}

func (m Model) extUIView() string {
	var b strings.Builder
	if m.extUI.title != "" {
		b.WriteString(m.titleStyle.Render(m.extUI.title))
		b.WriteByte('\n')
	}
	switch m.extUI.method {
	case "select", "confirm":
		for i, o := range m.extUI.options {
			mark := "  "
			if i == m.extUI.sel {
				mark = "→ "
			}
			b.WriteString(mark)
			b.WriteString(o)
			b.WriteByte('\n')
		}
	case "input":
		b.WriteString("> ")
		b.WriteString(m.extUI.input)
		b.WriteByte('\n')
	}
	b.WriteString(m.footerStyle.Render("Enter confirm · Esc cancel"))
	return b.String()
}

func (m Model) withTitle(s string) string {
	if m.extTitle == "" {
		return s
	}
	return "\x1b]0;" + m.extTitle + "\x07" + s
}

func strArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	s, _ := args[key].(string)
	return s
}

func stringList(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		var out []string
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
				continue
			}
			if m, ok := item.(map[string]any); ok {
				if l, _ := m["label"].(string); l != "" {
					out = append(out, l)
					continue
				}
				out = append(out, fmt.Sprint(m["value"]))
			}
		}
		return out
	}
	return nil
}

func (m Model) tryExtensionShortcut(msg tea.KeyMsg) (Model, bool) {
	if m.keys == nil {
		return m, false
	}
	if _, ok := m.keys.ExtensionKey(msg.String()); !ok {
		return m, false
	}
	if m.engine != nil {
		m.engine.DispatchShortcut(keys.Normalize(msg.String()))
	}
	return m, true
}
