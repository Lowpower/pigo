package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/slash"
)

type bashDoneMsg struct {
	command string
	exclude bool
	result  runtime.BashResult
}

func parseBang(text string) (command string, exclude, ok bool) {
	if !strings.HasPrefix(text, "!") {
		return "", false, false
	}
	exclude = strings.HasPrefix(text, "!!")
	if exclude {
		command = strings.TrimSpace(text[2:])
	} else {
		command = strings.TrimSpace(text[1:])
	}
	if command == "" {
		return "", false, false
	}
	return command, exclude, true
}

func (m Model) cwd() string {
	if m.completeDir != "" {
		return m.completeDir
	}
	if m.engine != nil && m.engine.Opts.Cwd != "" {
		return m.engine.Opts.Cwd
	}
	cwd, _ := os.Getwd()
	return cwd
}

func (m Model) extraSlashCommands() []slash.Command {
	if m.engine == nil {
		return nil
	}
	var extra []slash.Command
	for _, s := range m.engine.Skills {
		extra = append(extra, slash.Command{Name: s.Name, Description: s.Description})
	}
	for _, t := range m.engine.Templates {
		extra = append(extra, slash.Command{Name: t.Name, Description: t.Description})
	}
	return extra
}

func (m *Model) refreshComplete(force bool) {
	line, col := m.editor.cursorLC()
	before := textBeforeCursor(m.editor.Value(), line, col)
	cmds := slashCommands(m.extraSlashCommands())
	if items, prefix, ok := slashSuggestions(before, cmds); ok {
		m.complete.set(items, prefix)
		return
	}
	if items, prefix, ok := fileSuggestions(before, m.cwd(), force); ok {
		m.complete.set(items, prefix)
		return
	}
	m.complete.hide()
}

func (m Model) applyCompletion() (tea.Model, tea.Cmd) {
	item, ok := m.complete.current()
	if !ok {
		m.complete.hide()
		return m, nil
	}
	prefix := m.complete.prefix
	m.editor.applyComplete(prefix, item)
	m.complete.hide()
	if item.Dir {
		m.refreshComplete(true)
	}
	return m, nil
}

func (m Model) startBash(command string, exclude bool) (tea.Model, tea.Cmd) {
	if m.bashRunning {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("a bash command is already running (Esc to cancel)")})
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.bashCancel = cancel
	m.bashRunning = true
	cwd := m.cwd()
	return m, func() tea.Msg {
		res := runtime.RunBash(ctx, cwd, command, nil)
		return bashDoneMsg{command: command, exclude: exclude, result: res}
	}
}

func (m Model) handleBashDone(msg bashDoneMsg) (tea.Model, tea.Cmd) {
	m.bashRunning = false
	m.bashCancel = nil
	header := "$ " + msg.command
	if msg.exclude {
		header = "!! " + msg.command
	}
	body := strings.TrimRight(msg.result.Output, "\n")
	rendered := m.toolStyle.Render(header)
	if body != "" {
		rendered += "\n" + indent(body)
	}
	if msg.result.Cancelled {
		rendered += "\n" + m.errStyle.Render("(cancelled)")
	} else if msg.result.ExitCode != nil && *msg.result.ExitCode != 0 {
		rendered += "\n" + m.errStyle.Render(fmt.Sprintf("exit %d", *msg.result.ExitCode))
	}
	m.transcript = append(m.transcript, entry{role: "tool", rendered: rendered})
	if m.engine != nil {
		m.engine.PersistBash(msg.command, msg.result, msg.exclude)
	}
	if !msg.exclude {
		m.history = append(m.history, ai.Message{
			Role:    ai.RoleUser,
			Content: session.BashContextText(msg.command, msg.result.Output, msg.result.Cancelled, msg.result.ExitCode, msg.result.Truncated, msg.result.FullOutputPath),
		})
	}
	return m, nil
}
