package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
)

type sessionScope string

const (
	sessCurrent sessionScope = "current"
	sessAll     sessionScope = "all"
)

type sessionPicker struct {
	listPicker
	scope       sessionScope
	sort        session.SortMode
	names       session.NameFilter
	showPath    bool
	cwdList     []session.Summary
	allList     []session.Summary
	allLoaded   bool
	currentPath string
	confirm     string // path pending delete
	renaming    bool
	renameBuf   string
	status      string
	cwd         string
	agentDir    string
}

func (m Model) sessionPickerActive() bool { return m.sessions.active }

func (m Model) sessionCwd() string {
	if m.engine != nil && m.engine.Opts.Cwd != "" {
		return m.engine.Opts.Cwd
	}
	cwd, _ := os.Getwd()
	return cwd
}

func (m Model) openSessionPicker() (tea.Model, tea.Cmd) {
	cwd := m.sessionCwd()
	agentDir := configDir(m)
	list, err := session.Summaries(cwd, agentDir)
	if err != nil {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("resume error: " + err.Error())})
		return m, nil
	}
	p := sessionPicker{
		scope:      sessCurrent,
		sort:       session.SortThreaded,
		names:      session.NameAll,
		cwdList:    list,
		cwd:        cwd,
		agentDir:   agentDir,
		skipFilter: true,
	}
	if m.engine != nil && m.engine.Opts.Session != nil {
		p.currentPath = m.engine.Opts.Session.File()
	}
	p.active = true
	p.rebuild()
	m.sessions = p
	return m, nil
}

func configDir(m Model) string {
	if m.engine != nil && m.engine.Opts.AgentDir != "" {
		return m.engine.Opts.AgentDir
	}
	return config.DefaultConfigDir()
}

// ShouldOpenResumePicker is true for interactive `--resume` without a session id/path.
func ShouldOpenResumePicker(mode string, resume bool, sessionID, sessionPath, fork string, noSession bool) bool {
	return resume && mode == "interactive" && sessionID == "" && sessionPath == "" && fork == "" && !noSession
}

func (p *sessionPicker) source() []session.Summary {
	if p.scope == sessAll {
		return p.allList
	}
	return p.cwdList
}

func (p *sessionPicker) rebuild() {
	src := session.FilterSessions(p.source(), p.query, p.sort, p.names)
	var items []pickerItem
	if p.sort == session.SortThreaded && strings.TrimSpace(p.query) == "" {
		for _, row := range session.BuildThread(src) {
			items = append(items, p.item(row.Summary, row.Prefix))
		}
	} else {
		for _, s := range src {
			items = append(items, p.item(s, ""))
		}
	}
	p.setItems(items)
	p.filtered = items
	if p.selected >= len(p.filtered) {
		p.selected = max(0, len(p.filtered)-1)
	}
	p.title = p.header()
	p.hint = p.footer()
}

func (p sessionPicker) header() string {
	folder := "Resume Session (Current Folder)"
	scope := "◉ Current Folder | ○ All"
	if p.scope == sessAll {
		folder = "Resume Session (All)"
		scope = "○ Current Folder | ◉ All"
	}
	name := "All"
	if p.names == session.NameNamed {
		name = "Named"
	}
	sort := "Threaded"
	switch p.sort {
	case session.SortRecent:
		sort = "Recent"
	case session.SortFuzzy:
		sort = "Fuzzy"
	}
	return folder + "\n" + scope + "  Name: " + name + "  Sort: " + sort
}

func (p sessionPicker) footer() string {
	if p.confirm != "" {
		return "Delete session? Enter confirm · Esc cancel"
	}
	if p.renaming {
		return "Rename: " + p.renameBuf + "  (Enter save · Esc cancel)"
	}
	if p.status != "" {
		return p.status
	}
	path := "(off)"
	if p.showPath {
		path = "(on)"
	}
	if len(p.filtered) == 0 && p.scope == sessCurrent {
		return "No sessions in current folder. Tab for all · Esc cancel"
	}
	return "Tab scope · Ctrl+S sort · Ctrl+N named · Ctrl+D delete · Ctrl+R rename · Ctrl+P path " + path + " · Esc cancel"
}

func (p sessionPicker) item(s session.Summary, prefix string) pickerItem {
	label := strings.TrimSpace(s.Name)
	if label == "" {
		label = s.FirstMessage
	}
	if label == "" {
		label = s.ID
	}
	label = prefix + strings.ReplaceAll(label, "\n", " ")
	if p.currentPath != "" && filepath.Clean(s.Path) == filepath.Clean(p.currentPath) {
		label = "* " + label
	}
	right := strconv.Itoa(s.MessageCount) + " " + session.FormatAge(s.Modified, time.Now())
	if p.scope == sessAll && s.Cwd != "" {
		right = shortenHome(s.Cwd) + " " + right
	}
	if p.showPath {
		right = shortenHome(s.Path) + " " + right
	}
	return pickerItem{ID: s.Path, Label: label, Meta: right}
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + path[len(home):]
}

func (p *sessionPicker) ensureAll() {
	if p.allLoaded {
		return
	}
	list, err := session.SummariesAll(p.agentDir)
	if err == nil {
		p.allList = list
	}
	p.allLoaded = true
}

func (m Model) handleSessionPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.sessions
	if p.renaming {
		return m.handleSessionRenameKey(msg)
	}
	if p.confirm != "" {
		if m.keyIs(msg, "tui.select.confirm") {
			return m.deletePickedSession(p.confirm)
		}
		if m.keyIs(msg, "tui.select.cancel") {
			p.confirm = ""
			p.rebuild()
			return m, nil
		}
		return m, nil
	}
	if m.keyIs(msg, "app.session.toggleSort") {
		p.sort = session.NextSort(p.sort)
		p.rebuild()
		return m, nil
	}
	if m.keyIs(msg, "app.session.toggleNamedFilter") {
		if p.names == session.NameAll {
			p.names = session.NameNamed
		} else {
			p.names = session.NameAll
		}
		p.rebuild()
		return m, nil
	}
	if m.keyIs(msg, "app.session.togglePath") {
		p.showPath = !p.showPath
		p.rebuild()
		return m, nil
	}
	if m.keyIs(msg, "app.session.delete") || (m.keyIs(msg, "app.session.deleteNoninvasive") && p.query == "") {
		it, ok := p.current()
		if !ok {
			return m, nil
		}
		if p.currentPath != "" && filepath.Clean(it.ID) == filepath.Clean(p.currentPath) {
			p.status = "cannot delete the current session"
			p.rebuild()
			return m, nil
		}
		p.confirm = it.ID
		p.rebuild()
		return m, nil
	}
	if m.keyIs(msg, "app.session.rename") {
		it, ok := p.current()
		if !ok {
			return m, nil
		}
		p.renaming = true
		p.renameBuf = nameOf(it)
		p.rebuild()
		return m, nil
	}
	act := p.handleKey(msg, m.keys)
	switch act {
	case "tab":
		if p.scope == sessCurrent {
			p.ensureAll()
			p.scope = sessAll
		} else {
			p.scope = sessCurrent
		}
		p.rebuild()
		return m, nil
	case "cancel":
		m.sessions = sessionPicker{}
		return m, nil
	case "confirm":
		return m.applyPickedSession()
	case "edit":
		p.rebuild()
	}
	return m, nil
}

func nameOf(it pickerItem) string {
	s := strings.TrimPrefix(it.Label, "* ")
	if i := strings.Index(s, "└─ "); i >= 0 {
		s = s[i+len("└─ "):]
	}
	if i := strings.Index(s, "├─ "); i >= 0 {
		s = s[i+len("├─ "):]
	}
	return s
}

func (m Model) handleSessionRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.sessions
	if m.keyIs(msg, "tui.select.cancel") {
		p.renaming = false
		p.renameBuf = ""
		p.rebuild()
		return m, nil
	}
	if m.keyIs(msg, "tui.select.confirm") {
		it, ok := p.current()
		name := strings.TrimSpace(p.renameBuf)
		p.renaming = false
		p.renameBuf = ""
		if !ok || name == "" {
			p.rebuild()
			return m, nil
		}
		if err := session.UpdateHeader(it.ID, func(h *session.Header) { h.Name = name }); err != nil {
			p.status = "rename error: " + err.Error()
		} else {
			p.status = "renamed"
			p.reload()
		}
		p.rebuild()
		return m, nil
	}
	switch msg.String() {
	case "backspace":
		if p.renameBuf != "" {
			p.renameBuf = p.renameBuf[:len(p.renameBuf)-1]
		}
	default:
		if len(msg.Runes) > 0 && !msg.Alt {
			p.renameBuf += string(msg.Runes)
		}
	}
	p.rebuild()
	return m, nil
}

func (p *sessionPicker) reload() {
	if list, err := session.Summaries(p.cwd, p.agentDir); err == nil {
		p.cwdList = list
	}
	if p.allLoaded {
		if list, err := session.SummariesAll(p.agentDir); err == nil {
			p.allList = list
		}
	}
}

func (m Model) deletePickedSession(path string) (tea.Model, tea.Cmd) {
	p := &m.sessions
	p.confirm = ""
	if err := session.DeleteFile(path, p.currentPath); err != nil {
		p.status = "delete error: " + err.Error()
	} else {
		p.status = "deleted"
		p.reload()
	}
	p.rebuild()
	return m, nil
}

func (m Model) applyPickedSession() (tea.Model, tea.Cmd) {
	it, ok := m.sessions.current()
	m.sessions = sessionPicker{}
	if !ok {
		return m, nil
	}
	opened, err := session.Open(it.ID)
	if err != nil {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("resume error: " + err.Error())})
		return m, nil
	}
	if m.engine != nil {
		m.engine.AdoptSession(opened)
		m.history = m.engine.History()
	}
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("resumed " + opened.ID() + "\n" + opened.File())})
	return m, nil
}

func (m Model) handleResumeCommand(rest string) (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(s)})
		return m, nil
	}
	if m.engine == nil {
		return note("no engine")
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return m.openSessionPicker()
	}
	cwd := m.sessionCwd()
	opened, err := session.FindByID(cwd, m.engine.Opts.AgentDir, rest)
	if err != nil {
		opened, err = session.Open(rest)
	}
	if err != nil {
		if all, e2 := session.ListAll(m.engine.Opts.AgentDir); e2 == nil {
			for _, pth := range all {
				h, _, e3 := session.Load(pth)
				if e3 == nil && (h.ID == rest || strings.HasPrefix(h.ID, rest)) {
					opened, err = session.Open(pth)
					break
				}
			}
		}
	}
	if err != nil {
		return note("resume error: " + err.Error())
	}
	m.engine.AdoptSession(opened)
	m.history = m.engine.History()
	return note("resumed " + opened.ID() + "\n" + opened.File())
}
