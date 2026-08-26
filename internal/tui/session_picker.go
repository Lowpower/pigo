package tui

import (
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
)

type resumeOverlay struct {
	current   []session.Summary
	all       []session.Summary
	rows      []session.Summary
	scope     string // "current" or "all"
	cursor    int
	query     string
	cwd       string
	agent     string
	renaming  bool
	renameBuf string
	confirm   string // path pending delete
}

func (m Model) openResumePicker(cwd string) (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(s)})
		return m, nil
	}
	if m.engine == nil {
		return note("no engine")
	}
	cur, err := session.Summaries(cwd, m.engine.Opts.AgentDir)
	if err != nil {
		return note("resume error: " + err.Error())
	}
	all, err := session.SummariesAll(m.engine.Opts.AgentDir)
	if err != nil {
		return note("resume error: " + err.Error())
	}
	m.resume = resumeOverlay{
		current: cur,
		all:     all,
		scope:   "current",
		cwd:     cwd,
		agent:   m.engine.Opts.AgentDir,
	}
	m.resume.apply()
	if len(m.resume.rows) == 0 {
		return note("no sessions in this directory")
	}
	m.overlay = overlayResume
	return m, nil
}

func (r *resumeOverlay) apply() {
	src := r.current
	if r.scope == "all" {
		src = r.all
	}
	q := strings.ToLower(strings.TrimSpace(r.query))
	var rows []session.Summary
	for _, s := range src {
		hay := strings.ToLower(s.Name + " " + s.FirstMessage + " " + s.ID + " " + s.Cwd)
		if q == "" || strings.Contains(hay, q) {
			rows = append(rows, s)
		}
	}
	r.rows = rows
	if r.cursor >= len(r.rows) {
		r.cursor = max(0, len(r.rows)-1)
	}
}

func (m Model) handleResumeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.resume.renaming {
		switch key {
		case "enter":
			if m.resume.cursor >= 0 && m.resume.cursor < len(m.resume.rows) {
				path := m.resume.rows[m.resume.cursor].Path
				if opened, err := session.Open(path); err == nil {
					opened.SetName(strings.TrimSpace(m.resume.renameBuf))
				}
				m.resume.reload()
			}
			m.resume.renaming = false
			m.resume.renameBuf = ""
			return m, nil
		case "esc", "escape":
			m.resume.renaming = false
			m.resume.renameBuf = ""
			return m, nil
		case "backspace":
			if m.resume.renameBuf != "" {
				r := []rune(m.resume.renameBuf)
				m.resume.renameBuf = string(r[:len(r)-1])
			}
			return m, nil
		default:
			if isPrintableKey(key) {
				m.resume.renameBuf += key
			}
			return m, nil
		}
	}
	if m.resume.confirm != "" {
		switch key {
		case "y", "enter":
			_ = os.Remove(m.resume.confirm)
			m.resume.confirm = ""
			m.resume.reload()
			return m, nil
		default:
			m.resume.confirm = ""
			return m, nil
		}
	}
	switch key {
	case "up":
		if len(m.resume.rows) > 0 {
			m.resume.cursor = (m.resume.cursor - 1 + len(m.resume.rows)) % len(m.resume.rows)
		}
	case "down":
		if len(m.resume.rows) > 0 {
			m.resume.cursor = (m.resume.cursor + 1) % len(m.resume.rows)
		}
	case "tab":
		if m.resume.scope == "current" {
			m.resume.scope = "all"
		} else {
			m.resume.scope = "current"
		}
		m.resume.cursor = 0
		m.resume.apply()
	case "enter":
		if m.resume.cursor < 0 || m.resume.cursor >= len(m.resume.rows) {
			return m, nil
		}
		path := m.resume.rows[m.resume.cursor].Path
		if m.picking {
			m.pickResult = path
			m.quitting = true
			return m, tea.Quit
		}
		opened, err := session.Open(path)
		m.overlay = overlayNone
		if err != nil {
			m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("resume error: " + err.Error())})
			return m, nil
		}
		m.engine.AdoptSession(opened)
		m.reloadFromSession()
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("resumed " + opened.ID())})
		return m, nil
	case "esc", "escape", "ctrl+c":
		if m.resume.query != "" {
			m.resume.query = ""
			m.resume.apply()
			return m, nil
		}
		if m.picking {
			m.quitting = true
			return m, tea.Quit
		}
		m.overlay = overlayNone
		return m, nil
	case "n":
		if m.resume.cursor >= 0 && m.resume.cursor < len(m.resume.rows) {
			m.resume.renaming = true
			m.resume.renameBuf = m.resume.rows[m.resume.cursor].Name
		}
	case "d", "delete":
		if m.resume.cursor >= 0 && m.resume.cursor < len(m.resume.rows) {
			m.resume.confirm = m.resume.rows[m.resume.cursor].Path
		}
	case "backspace":
		if m.resume.query != "" {
			r := []rune(m.resume.query)
			m.resume.query = string(r[:len(r)-1])
			m.resume.apply()
		}
	default:
		if isPrintableKey(key) {
			m.resume.query += key
			m.resume.apply()
		}
	}
	return m, nil
}

func (r *resumeOverlay) reload() {
	if r.cwd != "" && r.agent != "" {
		r.current, _ = session.Summaries(r.cwd, r.agent)
		r.all, _ = session.SummariesAll(r.agent)
	}
	r.apply()
}

func (m Model) resumeView() string {
	var b strings.Builder
	b.WriteString(m.titleStyle.Render("  Resume Session"))
	b.WriteString("\n")
	b.WriteString(m.metaStyle.Render("tab scope (" + m.resume.scope + ")  enter open  n rename  d delete  type to search  esc"))
	b.WriteString("\nType to search: " + m.resume.query + "\n")
	if m.resume.renaming {
		b.WriteString("\n  Rename: " + m.resume.renameBuf + "█\n")
		return b.String()
	}
	if m.resume.confirm != "" {
		b.WriteString("\n  Delete " + m.resume.confirm + "? y/enter confirm, any other key cancel\n")
		return b.String()
	}
	maxLines := m.height / 2
	if maxLines < 5 {
		maxLines = 5
	}
	start := 0
	if m.resume.cursor >= maxLines {
		start = m.resume.cursor - maxLines + 1
	}
	end := min(len(m.resume.rows), start+maxLines)
	for i := start; i < end; i++ {
		s := m.resume.rows[i]
		cur := "  "
		if i == m.resume.cursor {
			cur = "› "
		}
		name := s.Name
		if name == "" {
			name = s.ID
			if len(name) > 8 {
				name = name[:8]
			}
		}
		line := cur + name + "  " + formatAge(s.Modified) + "  " + s.FirstMessage
		if m.resume.scope == "all" && s.Cwd != "" {
			line += "  (" + s.Cwd + ")"
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}

// PickSession runs a standalone picker and returns the selected path (empty if cancelled).
func PickSession(cwd, agentDir string) (string, error) {
	cur, err := session.Summaries(cwd, agentDir)
	if err != nil {
		return "", err
	}
	all, err := session.SummariesAll(agentDir)
	if err != nil {
		return "", err
	}
	m := New(config.Config{Theme: "default"})
	m.resume = resumeOverlay{current: cur, all: all, scope: "current", cwd: cwd, agent: agentDir}
	m.resume.apply()
	m.overlay = overlayResume
	m.picking = true
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", err
	}
	got, _ := final.(Model)
	return got.pickResult, nil
}
