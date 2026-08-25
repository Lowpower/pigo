package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
)

type loginPhase int

const (
	loginIdle loginPhase = iota
	loginPickType
	loginPickProvider
	loginPrompting
	loginRunning
)

type loginState struct {
	phase    loginPhase
	title    string
	lines    []string
	options  []loginOption
	selected int
	input    string
	secret   bool
	reply    chan string
	inbox    chan tea.Msg
	cancel   context.CancelFunc
	authType string
	provider string
}

type loginOption struct {
	id    string
	label string
}

type loginPromptMsg struct {
	prompt auth.Prompt
	reply  chan string
}

type loginEventMsg struct{ ev auth.Event }

type loginDoneMsg struct {
	err error
}

func (m Model) loginActive() bool { return m.login.phase != loginIdle }

func (m Model) startLogin(rest string) (tea.Model, tea.Cmd) {
	rest = strings.TrimSpace(rest)
	if rest != "" {
		matches := matchLoginProviders(rest)
		if len(matches) == 1 {
			p := matches[0]
			if p.OAuth != nil && (p.APIKey == nil || p.APIKey.Login == nil) {
				return m.beginProviderLogin(p.ID, auth.TypeOAuth)
			}
			if p.OAuth == nil {
				return m.beginProviderLogin(p.ID, auth.TypeAPIKey)
			}
			m.login = loginState{
				phase:    loginPickType,
				title:    "Select authentication method for " + p.ID + ":",
				provider: p.ID,
				options: []loginOption{
					{id: auth.TypeOAuth, label: oauthLabel(p)},
					{id: auth.TypeAPIKey, label: "Sign in with an API key"},
				},
			}
			return m, nil
		}
		if len(matches) == 0 {
			m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("unknown provider " + rest)})
			return m, nil
		}
	}
	m.login = loginState{
		phase: loginPickType,
		title: "Select authentication method:",
		options: []loginOption{
			{id: auth.TypeOAuth, label: "Sign in with an account"},
			{id: auth.TypeAPIKey, label: "Sign in with an API key"},
		},
	}
	return m, nil
}

func matchLoginProviders(q string) []auth.Provider {
	q = strings.ToLower(q)
	var out []auth.Provider
	for _, p := range auth.Providers() {
		if p.ID == q || strings.Contains(p.ID, q) {
			out = append(out, p)
		}
	}
	return out
}

func oauthLabel(p auth.Provider) string {
	if p.OAuth != nil && p.OAuth.LoginLabel() != "" {
		return p.OAuth.LoginLabel()
	}
	return "Sign in with an account"
}

func (m Model) beginProviderLogin(providerID, authType string) (tea.Model, tea.Cmd) {
	dir := config.DefaultConfigDir()
	if m.engine != nil {
		dir = m.engine.Opts.AgentDir
	}
	ctx, cancel := context.WithCancel(context.Background())
	inbox := make(chan tea.Msg, 8)
	m.login = loginState{
		phase:    loginRunning,
		title:    "Login to " + providerID,
		inbox:    inbox,
		cancel:   cancel,
		authType: authType,
		provider: providerID,
		lines:    []string{"starting…"},
	}
	store := auth.Open(dir)
	ix := auth.Interaction{
		Ctx: ctx,
		Prompt: func(p auth.Prompt) (string, error) {
			reply := make(chan string, 1)
			select {
			case inbox <- loginPromptMsg{prompt: p, reply: reply}:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			select {
			case v := <-reply:
				if v == "" && ctx.Err() != nil {
					return "", ctx.Err()
				}
				return v, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
		Notify: func(ev auth.Event) {
			select {
			case inbox <- loginEventMsg{ev}:
			case <-ctx.Done():
			}
		},
	}
	go func() {
		err := auth.Login(ctx, store, providerID, authType, ix)
		inbox <- loginDoneMsg{err: err}
		close(inbox)
	}()
	return m, waitLogin(inbox)
}

func waitLogin(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return loginDoneMsg{}
		}
		return msg
	}
}

func (m Model) handleLoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.login.phase {
	case loginPickType, loginPickProvider:
		switch msg.String() {
		case "up", "k":
			if m.login.selected > 0 {
				m.login.selected--
			}
		case "down", "j":
			if m.login.selected < len(m.login.options)-1 {
				m.login.selected++
			}
		case "enter":
			if m.login.reply != nil {
				id := m.login.options[m.login.selected].id
				m.login.reply <- id
				m.login.reply = nil
				m.login.phase = loginRunning
				return m, waitLogin(m.login.inbox)
			}
			return m.chooseLoginOption()
		case "esc", "escape":
			m.login = loginState{}
			return m, nil
		}
	case loginPrompting:
		switch msg.String() {
		case "enter":
			if m.login.reply != nil {
				m.login.reply <- m.login.input
				m.login.reply = nil
			}
			m.login.phase = loginRunning
			m.login.input = ""
			return m, waitLogin(m.login.inbox)
		case "esc", "escape":
			return m.cancelLogin()
		case "backspace":
			if m.login.input != "" {
				m.login.input = m.login.input[:len(m.login.input)-1]
			}
		default:
			if len(msg.Runes) > 0 {
				m.login.input += string(msg.Runes)
			}
		}
	case loginRunning:
		if msg.String() == "esc" || msg.String() == "escape" {
			return m.cancelLogin()
		}
	}
	return m, nil
}

func (m Model) chooseLoginOption() (tea.Model, tea.Cmd) {
	if m.login.selected < 0 || m.login.selected >= len(m.login.options) {
		return m, nil
	}
	opt := m.login.options[m.login.selected]
	switch m.login.phase {
	case loginPickType:
		if m.login.provider != "" {
			return m.beginProviderLogin(m.login.provider, opt.id)
		}
		var opts []loginOption
		for _, p := range auth.Providers() {
			if opt.id == auth.TypeOAuth && p.OAuth != nil {
				label := p.ID
				if p.OAuth.Name() != "" {
					label = p.OAuth.Name()
				}
				opts = append(opts, loginOption{id: p.ID, label: label})
			}
			if opt.id == auth.TypeAPIKey && p.APIKey != nil && p.APIKey.Login != nil {
				opts = append(opts, loginOption{id: p.ID, label: p.APIKey.Name})
			}
		}
		m.login.phase = loginPickProvider
		m.login.authType = opt.id
		m.login.options = opts
		m.login.selected = 0
		m.login.title = "Select provider:"
		return m, nil
	case loginPickProvider:
		return m.beginProviderLogin(opt.id, m.login.authType)
	}
	return m, nil
}

func (m Model) cancelLogin() (tea.Model, tea.Cmd) {
	if m.login.cancel != nil {
		m.login.cancel()
	}
	if m.login.reply != nil {
		m.login.reply <- ""
	}
	m.login = loginState{}
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("login cancelled")})
	return m, nil
}

func (m Model) handleLoginMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case loginEventMsg:
		line := formatLoginEvent(v.ev)
		m.login.lines = append(m.login.lines, line)
		return m, waitLogin(m.login.inbox)
	case loginPromptMsg:
		m.login.phase = loginPrompting
		m.login.reply = v.reply
		m.login.input = ""
		m.login.secret = v.prompt.Type == auth.PromptSecret
		m.login.title = v.prompt.Message
		if v.prompt.Type == auth.PromptSelect && len(v.prompt.Options) > 0 {
			m.login.phase = loginPickProvider
			m.login.options = nil
			for _, o := range v.prompt.Options {
				id := o.ID
				if id == "" {
					id = o.Label
				}
				m.login.options = append(m.login.options, loginOption{id: id, label: o.Label})
			}
			m.login.selected = 0
			// stash reply for enter
			m.login.reply = v.reply
		}
		return m, nil
	case loginDoneMsg:
		text := "login complete"
		if v.err != nil {
			text = "login error: " + v.err.Error()
		}
		m.login = loginState{}
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(text)})
		return m, nil
	}
	return m, nil
}

func formatLoginEvent(ev auth.Event) string {
	switch ev.Type {
	case auth.EventAuthURL:
		return "Open: " + ev.URL
	case auth.EventDeviceCode:
		return fmt.Sprintf("Code %s — %s", ev.UserCode, ev.VerificationURI)
	case auth.EventProgress, auth.EventInfo:
		return ev.Message
	default:
		return ev.Message
	}
}

func (m Model) loginView() string {
	var b strings.Builder
	b.WriteString(m.titleStyle.Render(m.login.title))
	b.WriteString("\n\n")
	for _, line := range m.login.lines {
		b.WriteString(m.metaStyle.Render(line))
		b.WriteString("\n")
	}
	if m.login.phase == loginPickType || m.login.phase == loginPickProvider {
		b.WriteString("\n")
		for i, o := range m.login.options {
			mark := "  "
			if i == m.login.selected {
				mark = "▸ "
			}
			b.WriteString(mark + o.label + "\n")
		}
		b.WriteString("\n" + m.footerStyle.Render("↑↓ select · Enter confirm · Esc cancel"))
		return b.String()
	}
	if m.login.phase == loginPrompting {
		shown := m.login.input
		if m.login.secret {
			shown = strings.Repeat("•", len(m.login.input))
		}
		b.WriteString("\n> " + shown + "\n")
		b.WriteString(m.footerStyle.Render("Enter submit · Esc cancel"))
		return b.String()
	}
	b.WriteString("\n" + m.footerStyle.Render("Esc cancel"))
	return b.String()
}
