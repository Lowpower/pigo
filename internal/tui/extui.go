package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/keys"
	"github.com/Lowpower/pigo/internal/theme"
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
type extEditorTextMsg struct{ text string }
type extPasteMsg struct{ text string }
type extFooterMsg struct {
	lines   []string
	restore bool
}
type extHeaderMsg struct {
	lines   []string
	restore bool
}
type extWorkingMsg struct {
	message    *string
	visible    *bool
	frames     []string
	framesSet  bool
	intervalMs int
}
type extThinkingLabelMsg struct {
	label   string
	restore bool
}
type extToolsSetMsg struct{ expanded bool }
type extCustomOpMsg struct {
	host *ext.Host
	op   string
	args map[string]any
}
type extTermSubMsg struct{ host *ext.Host }
type extKickMsg struct {
	user   string
	images []ai.ImageContent
}
type extShutdownMsg struct{}
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

func waitUIReply(reply chan map[string]any, timeout time.Duration) map[string]any {
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
	uiFor := func(host *ext.Host) func(string, map[string]any, time.Duration) map[string]any {
		return func(method string, args map[string]any, timeout time.Duration) map[string]any {
			if hub.send == nil {
				return map[string]any{"cancelled": true}
			}
			if args == nil {
				args = map[string]any{}
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
			case "set_editor_text":
				text, _ := args["text"].(string)
				hub.send(extEditorTextMsg{text: text})
				return map[string]any{}
			case "pasteToEditor":
				hub.send(extPasteMsg{text: strArg(args, "text")})
				return map[string]any{}
			case "setFooter":
				lines := stringList(args["lines"])
				hub.send(extFooterMsg{lines: lines, restore: len(lines) == 0})
				return map[string]any{}
			case "setHeader":
				lines := stringList(args["lines"])
				hub.send(extHeaderMsg{lines: lines, restore: len(lines) == 0})
				return map[string]any{}
			case "setWorkingMessage":
				text := strArg(args, "text")
				if text == "" {
					text = strArg(args, "message")
				}
				msg := text
				hub.send(extWorkingMsg{message: &msg})
				return map[string]any{}
			case "setWorkingVisible":
				v := true
				if _, ok := args["visible"]; ok {
					v = argBool(args, "visible")
				} else if _, ok := args["value"]; ok {
					v = argBool(args, "value")
				}
				hub.send(extWorkingMsg{visible: &v})
				return map[string]any{}
			case "setWorkingIndicator":
				if _, ok := args["frames"]; !ok {
					hub.send(extWorkingMsg{framesSet: true, frames: nil})
					return map[string]any{}
				}
				hub.send(extWorkingMsg{framesSet: true, frames: stringList(args["frames"]), intervalMs: argInt(args, "intervalMs")})
				return map[string]any{}
			case "setHiddenThinkingLabel":
				label := strArg(args, "label")
				if label == "" {
					label = strArg(args, "text")
				}
				hub.send(extThinkingLabelMsg{label: label, restore: label == ""})
				return map[string]any{}
			case "setToolsExpanded":
				hub.send(extToolsSetMsg{expanded: argBool(args, "expanded") || argBool(args, "value")})
				return map[string]any{}
			case "custom.open", "custom.update", "custom.close", "custom.focus", "custom.unfocus", "custom.hide", "custom.setHidden":
				hub.send(extCustomOpMsg{host: host, op: method, args: args})
				return map[string]any{"ok": true}
			case "terminal_input.subscribe":
				hub.send(extTermSubMsg{host: host})
				return map[string]any{"ok": true}
			case "select", "confirm", "input", "editor",
				"getEditorText", "getToolsExpanded", "getFooterData",
				"getAllThemes", "getTheme", "getCurrentTheme", "setTheme":
				reply := make(chan map[string]any, 1)
				hub.send(extUIReqMsg{method: method, args: args, reply: reply})
				return waitUIReply(reply, timeout)
			default:
				return map[string]any{"cancelled": true}
			}
		}
	}
	eng := m.engine
	for _, h := range m.engine.Hosts {
		if h == nil {
			continue
		}
		host := h
		h.SetUI(uiFor(host))
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
		h.SetShutdownRequest(func() {
			eng.HandleHostCall(host, "shutdown", nil)
			if hub.send != nil {
				hub.send(extShutdownMsg{})
			}
		})
	}
}

func argBool(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	switch t := args[key].(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	}
	return false
}

func argInt(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch t := args[key].(type) {
	case float64:
		return int(t)
	case int:
		return t
	}
	return 0
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
	case extEditorTextMsg:
		m.editor.SetValue(x.text)
		return m, true
	case extPasteMsg:
		m.editor.insertPaste(x.text)
		return m, true
	case extFooterMsg:
		if x.restore {
			m.extFooter = nil
			m.extFooterSet = false
		} else {
			m.extFooter = append([]string(nil), x.lines...)
			m.extFooterSet = true
		}
		return m, true
	case extHeaderMsg:
		if x.restore {
			m.extHeader = nil
		} else {
			m.extHeader = append([]string(nil), x.lines...)
		}
		return m, true
	case extWorkingMsg:
		if x.message != nil {
			m.workingMessage = *x.message
		}
		if x.visible != nil {
			m.workingVisible = x.visible
		}
		if x.framesSet {
			m.workingFrames = x.frames
			m.workingInterval = x.intervalMs
			if m.workingInterval <= 0 {
				m.workingInterval = 120
			}
		}
		return m, true
	case extThinkingLabelMsg:
		if x.restore {
			m.thinkingLabel = ""
		} else {
			m.thinkingLabel = x.label
		}
		return m, true
	case extToolsSetMsg:
		m.toolsExpanded = x.expanded
		return m, true
	case extTermSubMsg:
		if x.host != nil {
			m.termHosts = append(m.termHosts, x.host)
		}
		return m, true
	case extCustomOpMsg:
		m.applyCustomOp(x)
		return m, true
	case extUIReqMsg:
		switch x.method {
		case "getEditorText":
			if x.reply != nil {
				x.reply <- map[string]any{"text": m.editor.Value()}
			}
			return m, true
		case "getToolsExpanded":
			if x.reply != nil {
				x.reply <- map[string]any{"expanded": m.toolsExpanded}
			}
			return m, true
		case "getFooterData":
			if x.reply != nil {
				x.reply <- m.footerData()
			}
			return m, true
		case "getAllThemes", "getTheme", "getCurrentTheme":
			if x.reply != nil {
				x.reply <- m.handleThemeCall(x.method, x.args)
			}
			return m, true
		case "setTheme":
			res := m.applySetTheme(x.args)
			if x.reply != nil {
				x.reply <- res
			}
			return m, true
		}
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
		case "editor":
			m.extUI.input = strArg(x.args, "prefill")
			if m.extUI.input == "" {
				m.extUI.input = strArg(x.args, "value")
			}
		}
		return m, true
	}
	return m, false
}

func (m *Model) applyCustomOp(x extCustomOpMsg) {
	switch x.op {
	case "custom.open":
		s := &extCustomState{}
		s.applyOpen(x.host, x.args)
		m.extCustom = s
	case "custom.update":
		if m.extCustom != nil {
			m.extCustom.applyUpdate(x.args)
		}
	case "custom.close":
		m.extCustom = nil
	case "custom.focus":
		if m.extCustom != nil {
			m.extCustom.focused = true
		}
	case "custom.unfocus":
		if m.extCustom != nil {
			m.extCustom.focused = false
		}
	case "custom.hide":
		if m.extCustom != nil {
			m.extCustom.hidden = true
		}
	case "custom.setHidden":
		if m.extCustom != nil {
			m.extCustom.hidden = argBool(x.args, "hidden") || argBool(x.args, "value")
		}
	}
}

func (m Model) handleThemeCall(method string, args map[string]any) map[string]any {
	opt := m.themeOpts("")
	switch method {
	case "getAllThemes":
		var out []map[string]any
		for _, n := range theme.NamesWith(opt) {
			out = append(out, map[string]any{"name": n})
		}
		return map[string]any{"themes": out}
	case "getCurrentTheme":
		return themeSnapshot(m.theme)
	case "getTheme":
		name := strArg(args, "name")
		if name == "" {
			return themeSnapshot(m.theme)
		}
		return themeSnapshot(theme.LoadWith(m.themeOpts(name)))
	}
	return map[string]any{}
}

func (m *Model) applySetTheme(args map[string]any) map[string]any {
	if name := strArg(args, "name"); name != "" {
		m.applyTheme(theme.LoadWith(m.themeOpts(name)))
		m.cfg.Theme = name
		return map[string]any{"success": true, "name": m.theme.Name}
	}
	if colors, ok := args["colors"].(map[string]any); ok {
		th := m.theme
		if th.Colors == nil {
			th.Colors = map[string]string{}
		}
		for k, v := range colors {
			th.Colors[k] = fmt.Sprint(v)
		}
		m.applyTheme(th)
		return map[string]any{"success": true, "name": m.theme.Name}
	}
	return map[string]any{"success": false, "error": "name or colors required"}
}

func themeSnapshot(th theme.Theme) map[string]any {
	return map[string]any{
		"name": th.Name, "user": th.User, "assistant": th.Assistant,
		"tool": th.Tool, "error": th.Error, "muted": th.Muted, "accent": th.Accent,
		"colors": th.Colors,
	}
}

func (m Model) footerData() map[string]any {
	var statuses []string
	for _, v := range m.extStatus {
		if v != "" {
			statuses = append(statuses, v)
		}
	}
	cwd := m.gitCwd
	if cwd == "" && m.engine != nil {
		cwd = m.engine.Opts.Cwd
	}
	modelID := m.cfg.ResolvedModel()
	return map[string]any{
		"gitBranch": m.gitBranch,
		"statuses":  statuses,
		"tokens":    m.usage.Input + m.usage.Output,
		"cost":      m.usage.Cost.Total,
		"modelId":   modelID,
		"cwd":       cwd,
	}
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
	case "input", "editor":
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
	case "input", "editor":
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
