package tui

import (
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type forkMsg struct {
	id   string
	text string
}

type forkOverlay struct {
	msgs   []forkMsg
	cursor int
}

func (m Model) openFork() (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(s)})
		return m, nil
	}
	if m.engine == nil || m.engine.Opts.Session == nil {
		return note("no session")
	}
	raw := m.engine.Opts.Session.UserMessagesForForking()
	if len(raw) == 0 {
		return note("No messages to fork from")
	}
	msgs := make([]forkMsg, 0, len(raw))
	for _, row := range raw {
		msgs = append(msgs, forkMsg{id: row["entryId"], text: row["text"]})
	}
	m.fork = forkOverlay{msgs: msgs, cursor: len(msgs) - 1}
	m.overlay = overlayFork
	return m, nil
}

func (m Model) handleForkKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if len(m.fork.msgs) > 0 {
			m.fork.cursor = (m.fork.cursor - 1 + len(m.fork.msgs)) % len(m.fork.msgs)
		}
	case "down":
		if len(m.fork.msgs) > 0 {
			m.fork.cursor = (m.fork.cursor + 1) % len(m.fork.msgs)
		}
	case "enter":
		return m.confirmFork()
	case "esc", "escape", "ctrl+c":
		m.overlay = overlayNone
		return m, nil
	}
	return m, nil
}

func (m Model) confirmFork() (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.overlay = overlayNone
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(s)})
		return m, nil
	}
	if m.fork.cursor < 0 || m.fork.cursor >= len(m.fork.msgs) {
		return note("No messages to fork from")
	}
	if m.engine == nil || m.engine.Opts.Session == nil {
		return note("no session")
	}
	cwd, _ := os.Getwd()
	if m.engine.Opts.Cwd != "" {
		cwd = m.engine.Opts.Cwd
	}
	sel := m.fork.msgs[m.fork.cursor]
	child, text, err := m.engine.Opts.Session.ForkFrom(sel.id, cwd, m.engine.Opts.AgentDir, "before")
	if err != nil {
		return note("fork error: " + err.Error())
	}
	m.engine.AdoptSession(child)
	m.reloadFromSession()
	if text != "" {
		m.editor.SetValue(text)
	}
	m.overlay = overlayNone
	return note("Forked to new session " + child.ID() + "\n" + child.File())
}

func (m Model) forkView() string {
	var b strings.Builder
	b.WriteString(m.titleStyle.Render("  Fork from message"))
	b.WriteString("\n")
	b.WriteString(m.metaStyle.Render("↑↓ select  enter fork  esc cancel"))
	b.WriteString("\n")
	if len(m.fork.msgs) == 0 {
		b.WriteString(m.metaStyle.Render("  No user messages found\n"))
		return b.String()
	}
	maxVisible := 10
	start := max(0, min(m.fork.cursor-maxVisible/2, len(m.fork.msgs)-maxVisible))
	end := min(len(m.fork.msgs), start+maxVisible)
	for i := start; i < end; i++ {
		msg := m.fork.msgs[i]
		cur := "  "
		if i == m.fork.cursor {
			cur = "› "
		}
		line := strings.ReplaceAll(strings.TrimSpace(msg.text), "\n", " ")
		b.WriteString(cur + line + "\n")
		b.WriteString(m.metaStyle.Render("  Message " + strconv.Itoa(i+1) + " of " + strconv.Itoa(len(m.fork.msgs))))
		b.WriteString("\n\n")
	}
	return b.String()
}
