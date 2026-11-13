package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/mcp"
	"github.com/Lowpower/pigo/internal/mcp/oauth"
	"github.com/Lowpower/pigo/internal/runtime"
)

const mcpLoginTimeout = 300 * time.Second

type mcpScreen struct {
	active     bool
	gen        int
	cursor     int
	rows       []mcp.Status
	status     string
	authURL    string
	copyHint   string
	copyFailed bool
	inbox      chan tea.Msg
	cancel     context.CancelFunc
}

type mcpTickMsg struct{ gen int }

type mcpAuthURLMsg struct{ url string }

type mcpLoginDoneMsg struct{ err error }

func (m Model) mcpActive() bool { return m.mcp.active }

func (m Model) openMCP() (tea.Model, tea.Cmd) {
	m.mcp = mcpScreen{active: true, gen: m.mcp.gen + 1}
	m.mcp.load(m.engine)
	return m, mcpTick(m.mcp.gen)
}

func (s *mcpScreen) load(e *runtime.Engine) {
	if e == nil {
		s.rows = nil
		return
	}
	s.rows = e.MCPStatuses()
	if s.cursor >= len(s.rows) {
		s.cursor = 0
	}
}

func mcpTick(gen int) tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return mcpTickMsg{gen: gen}
	})
}

func (m Model) mcpView() string {
	var b strings.Builder
	b.WriteString(m.titleStyle.Render("MCP"))
	b.WriteByte('\n')
	if len(m.mcp.rows) == 0 {
		b.WriteString("No MCP servers.\n")
	}
	for i, row := range m.mcp.rows {
		mark := "  "
		if i == m.mcp.cursor {
			mark = "> "
		}
		line := fmt.Sprintf("%s%s  %s  %s", mark, row.Name, row.Status, row.Exposure)
		if row.HasResources {
			line += "  resources"
		}
		if row.Err != "" && row.Status != "disabled" {
			line += "  " + row.Err
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if m.mcp.authURL != "" {
		b.WriteString(m.authURLBlock(m.mcp.authURL, m.mcp.copyHint, m.mcp.copyFailed))
	}
	if m.mcp.status != "" {
		b.WriteString(m.metaStyle.Render(m.mcp.status))
		b.WriteByte('\n')
	}
	b.WriteString(m.footerStyle.Render("j/k move · e enable · d disable · r reconnect · l login · esc close"))
	b.WriteByte('\n')
	return b.String()
}

func (m Model) handleMCPKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mcp.authURL != "" && m.keyIs(msg, "app.message.copy") {
		osc, hint, failed := m.copyURL(m.mcp.authURL)
		m.clipOSC = osc
		m.mcp.copyHint = hint
		m.mcp.copyFailed = failed
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		if m.mcp.cursor > 0 {
			m.mcp.cursor--
		}
	case "down", "j":
		if m.mcp.cursor+1 < len(m.mcp.rows) {
			m.mcp.cursor++
		}
	case "esc", "escape":
		if m.mcp.cancel != nil {
			m.mcp.cancel()
			m.mcp.cancel = nil
			m.mcp.authURL = ""
			m.mcp.copyHint = ""
			m.mcp.status = "login canceled"
			return m, nil
		}
		m.mcp.active = false
		return m, nil
	case "e":
		m.mcp.status = m.setMCPEnabled(true)
		m.mcp.load(m.engine)
	case "d":
		m.mcp.status = m.setMCPEnabled(false)
		m.mcp.load(m.engine)
	case "r":
		m.mcp.status = m.reconnectMCP()
		m.mcp.load(m.engine)
	case "l":
		return m.startMCPLogin()
	}
	return m, nil
}

func (m Model) selectedMCP() (mcp.Status, bool) {
	if m.mcp.cursor < 0 || m.mcp.cursor >= len(m.mcp.rows) {
		return mcp.Status{}, false
	}
	return m.mcp.rows[m.mcp.cursor], true
}

func (m Model) setMCPEnabled(enabled bool) string {
	row, ok := m.selectedMCP()
	if !ok {
		return "no MCP server"
	}
	if m.engine == nil {
		return "mcp is not loaded"
	}
	if err := m.engine.SetMCPEnabled(row.Name, enabled); err != nil {
		return err.Error()
	}
	if enabled {
		return "enabled " + row.Name
	}
	return "disabled " + row.Name
}

func (m Model) reconnectMCP() string {
	row, ok := m.selectedMCP()
	if !ok {
		return "no MCP server"
	}
	if m.engine == nil {
		return "mcp is not loaded"
	}
	if err := m.engine.ReconnectMCP(row.Name); err != nil {
		return err.Error()
	}
	return "reconnecting " + row.Name
}

func (m Model) startMCPLogin() (tea.Model, tea.Cmd) {
	row, ok := m.selectedMCP()
	if !ok || m.engine == nil {
		m.mcp.status = "mcp is not loaded"
		return m, nil
	}
	srv, found := m.engine.MCPServer(row.Name)
	if !found || srv.Config.URL == "" {
		m.mcp.status = row.Name + " has no url"
		return m, nil
	}
	if m.mcp.cancel != nil {
		m.mcp.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpLoginTimeout)
	inbox := make(chan tea.Msg, 4)
	m.mcp.cancel = cancel
	m.mcp.inbox = inbox
	m.mcp.authURL = ""
	m.mcp.copyHint = ""
	m.mcp.status = "signing in to " + row.Name
	eng := m.engine
	name := row.Name
	go func() {
		err := eng.MCPLogin(ctx, name, oauth.Prompt{
			Notify: func(ev auth.Event) {
				if ev.URL == "" {
					return
				}
				select {
				case inbox <- mcpAuthURLMsg{url: ev.URL}:
				case <-ctx.Done():
				}
			},
		})
		select {
		case inbox <- mcpLoginDoneMsg{err: err}:
		case <-ctx.Done():
		}
	}()
	return m, waitMCP(inbox)
}

func waitMCP(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return mcpLoginDoneMsg{}
		}
		return msg
	}
}

func (m Model) handleMCPMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case mcpTickMsg:
		if !m.mcp.active || msg.gen != m.mcp.gen {
			return m, nil
		}
		m.mcp.load(m.engine)
		return m, mcpTick(m.mcp.gen)
	case mcpAuthURLMsg:
		if !m.mcp.active {
			return m, waitMCP(m.mcp.inbox)
		}
		m.mcp.authURL = msg.url
		m.mcp.copyHint = ""
		m.mcp.copyFailed = false
		return m, waitMCP(m.mcp.inbox)
	case mcpLoginDoneMsg:
		m.mcp.cancel = nil
		if msg.err != nil {
			m.mcp.status = msg.err.Error()
		} else {
			m.mcp.status = "signed in"
		}
		m.mcp.load(m.engine)
		return m, nil
	default:
		return m, nil
	}
}
