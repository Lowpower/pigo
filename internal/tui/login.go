package tui

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
)

type loginPhase int

const (
	loginIdle loginPhase = iota
	loginPickType
	loginPickProvider
	loginPrompting
	loginRunning
	logoutPick
)

type loginState struct {
	phase      loginPhase
	title      string
	lines      []string
	options    []loginOption
	selected   int
	input      string
	secret     bool
	reply      chan string
	inbox      chan tea.Msg
	cancel     context.CancelFunc
	authType   string
	provider   string
	authURL    string
	copyHint   string
	copyFailed bool
}

type loginOption struct {
	id    string
	label string
	kind  string
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
			m.appendMeta("unknown provider " + rest)
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
	var exact, partial []auth.Provider
	for _, p := range auth.Providers() {
		id := strings.ToLower(p.ID)
		if id == q {
			exact = append(exact, p)
			continue
		}
		if strings.Contains(id, q) {
			partial = append(partial, p)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
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
		DeviceID: func() (string, error) {
			return config.GetOrCreateDeviceID(dir)
		},
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
	if m.login.authURL != "" && m.keyIs(msg, "app.message.copy") {
		return m.copyLoginAuthURL()
	}
	switch m.login.phase {
	case loginPickType, loginPickProvider, logoutPick:
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
			if m.login.phase == logoutPick {
				return m.confirmLogout()
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
		store := auth.Open(configDir(m))
		var opts []loginOption
		for _, p := range auth.Providers() {
			chk := auth.CheckAuth(store, p.ID)
			sub := providerSubscription(p)
			if opt.id == auth.TypeOAuth && p.OAuth != nil {
				label := p.ID
				if p.OAuth.Name() != "" {
					label = p.OAuth.Name()
				}
				opts = append(opts, loginOption{id: p.ID, label: formatAuthRow(label, auth.TypeOAuth, sub, chk)})
			}
			if opt.id == auth.TypeAPIKey && p.APIKey != nil && p.APIKey.Login != nil {
				opts = append(opts, loginOption{id: p.ID, label: formatAuthRow(p.APIKey.Name, auth.TypeAPIKey, sub, chk)})
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
	m.appendMeta("login cancelled")
	return m, nil
}

func (m Model) handleLoginMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case loginEventMsg:
		if v.ev.Type == auth.EventAuthURL {
			if v.ev.URL != "" {
				m.login.authURL = v.ev.URL
				m.login.copyHint = ""
				m.login.copyFailed = false
			}
			return m, waitLogin(m.login.inbox)
		}
		if line := formatLoginEvent(v.ev); line != "" {
			m.login.lines = append(m.login.lines, line)
		}
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
		provider := m.login.provider
		text := "login complete"
		if v.err != nil {
			text = "login error: " + v.err.Error()
		}
		m.login = loginState{}
		m.appendMeta(text)
		if v.err == nil {
			m.maybeWarnAnthropicExtraUsage()
			m.maybeWarnRadiusCatalog(provider)
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) maybeWarnRadiusCatalog(provider string) {
	if provider != "radius" {
		return
	}
	id := models.DefaultID("radius")
	if id == "" {
		id = "balanced"
	}
	if _, ok := models.Lookup("radius", id); ok {
		return
	}
	m.appendMeta("Radius model catalog timed out; models may be unavailable until refresh.")
}

func formatLoginEvent(ev auth.Event) string {
	switch ev.Type {
	case auth.EventAuthURL:
		return ""
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
	if block := m.loginAuthBlock(); block != "" {
		b.WriteString(block)
		b.WriteString("\n")
	}
	for _, line := range m.login.lines {
		b.WriteString(m.metaStyle.Render(line))
		b.WriteString("\n")
	}
	if m.login.phase == loginPickType || m.login.phase == loginPickProvider || m.login.phase == logoutPick {
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

func (m Model) copyLoginAuthURL() (tea.Model, tea.Cmd) {
	osc, hint, failed := m.copyURL(m.login.authURL)
	m.clipOSC = osc
	m.login.copyHint = hint
	m.login.copyFailed = failed
	return m, nil
}

func (m Model) copyURL(url string) (osc, hint string, failed bool) {
	copied, err := copyToClipboard(url, clipboardGOOS, clipboardGetenv)
	if err != nil {
		return "", err.Error(), true
	}
	return copied, "Copied URL to clipboard", false
}

func (m Model) loginAuthBlock() string {
	return m.authURLBlock(m.login.authURL, m.login.copyHint, m.login.copyFailed)
}

func (m Model) authURLBlock(url, hint string, failed bool) string {
	if url == "" {
		return ""
	}
	shown := m.titleStyle.Render(url)
	click := m.metaStyle.Render(loginClickHint())
	if m.cfg.HyperlinksEnabled(true) {
		shown = osc8(shown, url)
		click = osc8(click, url)
	}
	suffix := hint
	if suffix == "" {
		suffix = startupKeyHint(m.keys, "app.message.copy", "to copy")
	}
	suffixStyle := m.metaStyle
	if failed {
		suffixStyle = m.errStyle
	}
	var b strings.Builder
	b.WriteString(shown)
	b.WriteString("\n")
	b.WriteString(click)
	b.WriteString(m.metaStyle.Render(" • "))
	b.WriteString(suffixStyle.Render(suffix))
	b.WriteString("\n")
	return b.String()
}

func loginClickHint() string {
	if runtime.GOOS == "darwin" {
		return "Cmd+click to open"
	}
	return "Ctrl+click to open"
}

// envCredentialSource matches pi v1.3.0: an env var name, or several separated by ", ".
var envCredentialSource = regexp.MustCompile(`^[A-Z][A-Z0-9_]*(?:, [A-Z][A-Z0-9_]*)*$`)

const logoutEmptyNotice = "No stored credentials to remove. /logout only removes credentials saved by /login; environment variables and models.json config are unchanged."

func authTypeWord(authType string, subscription bool) string {
	if authType == auth.TypeAPIKey {
		return "API key"
	}
	if !subscription {
		return "account"
	}
	return "subscription"
}

func authStatusSuffix(rowType string, subscription bool, chk *auth.Check) string {
	if chk == nil {
		return " • not configured"
	}
	if chk.Type != rowType {
		return " • " + authTypeWord(chk.Type, subscription) + " configured"
	}
	src := chk.Source
	if src == "" || src == "OAuth" || src == "stored credential" {
		return " ✓ configured"
	}
	if envCredentialSource.MatchString(src) {
		return " ✓ env: " + src
	}
	return " ✓ " + src
}

func formatAuthRow(name, authType string, subscription bool, chk *auth.Check) string {
	return name + " [" + authTypeWord(authType, subscription) + "]" + authStatusSuffix(authType, subscription, chk)
}

func providerSubscription(p auth.Provider) bool {
	return p.OAuth != nil && p.OAuth.IsSubscription()
}

func providerMethodName(p auth.Provider, authType string) string {
	if authType == auth.TypeOAuth && p.OAuth != nil && p.OAuth.Name() != "" {
		return p.OAuth.Name()
	}
	if p.APIKey != nil && p.APIKey.Name != "" {
		return p.APIKey.Name
	}
	if p.OAuth != nil && p.OAuth.Name() != "" {
		return p.OAuth.Name()
	}
	return p.ID
}

func (m Model) startLogout(rest string) (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.appendMeta(s)
		return m, nil
	}
	dir := configDir(m)
	rest = strings.TrimSpace(rest)
	if rest != "" {
		if err := auth.Delete(dir, rest); err != nil {
			return note("logout error: " + err.Error())
		}
		return note("removed stored key for " + rest)
	}
	infos, err := auth.Open(dir).List()
	if err != nil {
		return note("logout error: " + err.Error())
	}
	if len(infos) == 0 {
		return note(logoutEmptyNotice)
	}
	opts := make([]loginOption, 0, len(infos))
	for _, info := range infos {
		name := info.ProviderID
		sub := false
		if p, ok := auth.Lookup(info.ProviderID); ok {
			sub = providerSubscription(p)
			name = providerMethodName(p, info.Type)
		}
		opts = append(opts, loginOption{
			id:    info.ProviderID,
			kind:  info.Type,
			label: formatAuthRow(name, info.Type, sub, &auth.Check{Type: info.Type, Source: "stored credential"}),
		})
	}
	sort.Slice(opts, func(i, j int) bool { return opts[i].label < opts[j].label })
	m.login = loginState{
		phase:   logoutPick,
		title:   "Select provider to logout:",
		options: opts,
	}
	return m, nil
}

func (m Model) confirmLogout() (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.appendMeta(s)
		return m, nil
	}
	if m.login.selected < 0 || m.login.selected >= len(m.login.options) {
		m.login = loginState{}
		return m, nil
	}
	opt := m.login.options[m.login.selected]
	if err := auth.Delete(configDir(m), opt.id); err != nil {
		m.login = loginState{}
		return note("logout error: " + err.Error())
	}
	m.login = loginState{}
	return note(logoutDoneText(opt.id, opt.kind))
}

func logoutDoneText(id, kind string) string {
	name := id
	if p, ok := auth.Lookup(id); ok {
		name = providerMethodName(p, kind)
	}
	if kind == auth.TypeOAuth {
		return "Logged out of " + name
	}
	return "Removed stored API key for " + name + ". Environment variables and models.json config are unchanged."
}
