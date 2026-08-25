package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/skills"
	"github.com/Lowpower/pigo/internal/slash"
	"github.com/Lowpower/pigo/internal/theme"
	"github.com/Lowpower/pigo/internal/tools"
)

const maxEditorWidth = 100

// entry is one rendered line/block in the transcript.
type entry struct {
	role     string
	rendered string
}

// agentEventMsg / agentClosedMsg carry agent-loop events into the bubbletea loop.
type agentEventMsg struct{ ev agent.Event }
type agentClosedMsg struct{}

// Model is the interactive TUI: it drives the agent loop (internal/agent)
// with real tools (internal/tools) and a provider (internal/ai), streaming the
// assistant response to screen live (plain text during the turn), then rendering
// it as markdown via glamour once the turn ends. Keybindings follow pi.
type Model struct {
	cfg      config.Config
	engine   *runtime.Engine
	theme    theme.Theme
	textarea textarea.Model

	transcript []entry
	history    []ai.Message // raw user/assistant messages carried across turns
	queued     []string     // follow-up prompts typed while a turn is running (pi follow-up)

	streaming       string // in-progress assistant text (plain)
	streamingActive bool
	running         bool
	provider        string

	agentEvents <-chan agent.Event
	cancel      context.CancelFunc

	width, height int
	quitting      bool

	glam *glamour.TermRenderer

	titleStyle  lipgloss.Style
	metaStyle   lipgloss.Style
	userStyle   lipgloss.Style
	toolStyle   lipgloss.Style
	errStyle    lipgloss.Style
	streamStyle lipgloss.Style
	footerStyle lipgloss.Style
}

// New builds the interactive model from the resolved config.
func New(cfg config.Config) Model {
	ta := textarea.New()
	ta.Placeholder = "Ask pigo…  (Enter to send, Shift+Enter/Ctrl+J newline, Ctrl+C interrupt/quit)"
	ta.Prompt = "│ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	ta.Focus()

	th := theme.Load(cfg.Theme, "", "")
	m := Model{
		cfg:         cfg,
		theme:       th,
		textarea:    ta,
		titleStyle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(th.Accent)),
		metaStyle:   lipgloss.NewStyle().Faint(true),
		userStyle:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(th.User)),
		toolStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color(th.Tool)),
		errStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color(th.Error)),
		streamStyle: lipgloss.NewStyle().Foreground(lipgloss.Color(th.Assistant)),
		footerStyle: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(th.Muted)),
	}
	m.glam = newRenderer(80)
	return m
}

func newRenderer(width int) *glamour.TermRenderer {
	wrap := width - 2
	if wrap < 20 {
		wrap = 20
	}
	r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(wrap))
	if err != nil {
		return nil
	}
	return r
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return textarea.Blink }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.textarea.SetWidth(min(msg.Width, maxEditorWidth))
		m.glam = newRenderer(min(msg.Width, maxEditorWidth))
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.running && m.cancel != nil {
				m.cancel() // interrupt the current run; keep the UI open
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		case "ctrl+d":
			if strings.TrimSpace(m.textarea.Value()) == "" {
				m.quitting = true
				return m, tea.Quit
			}
		case "esc", "escape":
			if m.running && m.cancel != nil {
				m.cancel()
				return m, nil
			}
		case "enter":
			return m.submit()
		case "alt+enter":
			return m.queueFollowUp()
		case "ctrl+p":
			return m.cycleModel(false)
		case "ctrl+shift+p", "shift+ctrl+p":
			return m.cycleModel(true)
		case "shift+tab":
			return m.cycleThinking()
		}

	case agentEventMsg:
		m.applyAgentEvent(msg.ev)
		return m, waitForAgentEvent(m.agentEvents)

	case agentClosedMsg:
		m.running = false
		m.streamingActive = false
		m.streaming = ""
		m.agentEvents = nil
		m.cancel = nil
		if len(m.queued) > 0 {
			next := m.queued[0]
			m.queued = m.queued[1:]
			return m.startTurn(next)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m Model) queueFollowUp() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return m, nil
	}
	m.textarea.Reset()
	if !m.running {
		return m.startTurn(text)
	}
	if m.engine != nil {
		m.engine.PushFollow(text)
	} else {
		m.queued = append(m.queued, text)
	}
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("follow-up: " + text)})
	return m, nil
}

func (m Model) cycleModel(backward bool) (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(s)})
		return m, nil
	}
	if m.engine == nil {
		return note("no engine to cycle models")
	}
	next, ok := m.engine.CycleModel(backward)
	if !ok {
		return note("only one model available")
	}
	m.cfg = m.engine.Opts.Config
	m.provider = m.engine.Provider
	return note("model = " + next.Provider + "/" + next.ID)
}

func (m Model) cycleThinking() (tea.Model, tea.Cmd) {
	if m.engine != nil {
		level := m.engine.CycleThinking()
		m.cfg = m.engine.Opts.Config
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("thinking = " + level)})
		return m, nil
	}
	m.cfg.Thinking = models.NextThinkingLevel(m.cfg.Thinking)
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("thinking = " + m.cfg.Thinking)})
	return m, nil
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return m, nil
	}
	if m.running {
		m.textarea.Reset()
		// pi: Enter while streaming steers the current loop; leftover lines
		// after the turn are follow-ups (drained on agentClosedMsg).
		if m.engine != nil {
			m.engine.PushSteer(text)
			m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("steering: " + text)})
			return m, nil
		}
		m.queued = append(m.queued, text)
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("queued: " + text)})
		return m, nil
	}
	if cmd, ok := slash.Parse(text); ok {
		return m.handleSlash(cmd)
	}
	return m.startTurn(text)
}

func (m Model) handleSlash(cmd slash.Command) (tea.Model, tea.Cmd) {
	m.textarea.Reset()
	note := func(s string) (tea.Model, tea.Cmd) {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(s)})
		return m, nil
	}
	switch cmd.Name {
	case "quit":
		m.quitting = true
		return m, tea.Quit
	case "help":
		return note(slash.HelpText())
	case "hotkeys":
		return note(slash.HotkeysText())
	case "clear":
		m.transcript = nil
		return m, nil
	case "session":
		if m.engine != nil && m.engine.Opts.Session != nil {
			return note("session " + m.engine.Opts.Session.ID() + "\n" + m.engine.Opts.Session.File())
		}
		return note("no session (started with --no-session)")
	case "model":
		if cmd.Rest != "" {
			prov, id, thinking := models.ParseSpec(cmd.Rest)
			if m.engine != nil {
				m.engine.ApplyModel(prov, id, thinking)
				m.cfg = m.engine.Opts.Config
				m.provider = m.engine.Provider
			} else {
				if prov != "" {
					m.cfg.Provider, m.cfg.DefaultProvider = prov, prov
				}
				m.cfg.Model, m.cfg.DefaultModel = id, id
				if thinking != "" {
					m.cfg.Thinking = thinking
				}
			}
		}
		return note("model = " + m.cfg.ResolvedProvider() + "/" + m.cfg.ResolvedModel())
	case "provider":
		if cmd.Rest != "" {
			m.cfg.Provider = cmd.Rest
			m.cfg.DefaultProvider = cmd.Rest
			if m.engine != nil {
				m.engine.ApplyModel(cmd.Rest, "", "")
			}
		}
		return note("provider = " + m.cfg.ResolvedProvider())
	case "theme":
		if cmd.Rest != "" {
			m.cfg.Theme = cmd.Rest
			m.theme = theme.Load(cmd.Rest, "", "")
		}
		return note("theme = " + m.theme.Name)
	case "thinking":
		if cmd.Rest != "" {
			m.cfg.Thinking = cmd.Rest
			if m.engine != nil {
				m.engine.Opts.Config.Thinking = cmd.Rest
			}
		}
		return note("thinking = " + m.cfg.Thinking)
	case "tools":
		var names []string
		reg := tools.Default()
		if m.engine != nil {
			reg = m.engine.Tools
		}
		for _, t := range reg.List() {
			names = append(names, t.Name())
		}
		return note("tools: " + strings.Join(names, ", "))
	case "skills":
		if m.engine == nil || len(m.engine.Skills) == 0 {
			return note("no skills discovered")
		}
		var names []string
		for _, s := range m.engine.Skills {
			names = append(names, s.Name)
		}
		return note("skills: " + strings.Join(names, ", "))
	case "new":
		m.history = nil
		m.transcript = nil
		if m.engine != nil {
			cwd, _ := os.Getwd()
			s := session.New(cwd, m.engine.Opts.AgentDir)
			m.engine.AdoptSession(s)
		}
		return note("started a new session")
	case "compact":
		if m.engine == nil {
			return note("compaction requires a runtime engine")
		}
		out, summary, err := m.engine.MaybeCompact(context.Background(), m.history)
		if err != nil {
			return note("compact error: " + err.Error())
		}
		if summary == "" {
			return note("compaction not needed")
		}
		m.history = out
		return note("compacted history")
	case "settings":
		return note("settings dir: " + config.DefaultConfigDir())
	case "export":
		if m.engine == nil || m.engine.Opts.Session == nil {
			return note("no session to export")
		}
		dest := strings.TrimSpace(cmd.Rest)
		if dest == "" || strings.HasSuffix(strings.ToLower(dest), ".html") {
			path, err := session.ExportHTML(m.engine.Opts.Session, dest)
			if err != nil {
				return note("export error: " + err.Error())
			}
			return note("exported html: " + path)
		}
		b, err := os.ReadFile(m.engine.Opts.Session.File())
		if err != nil {
			return note("export error: " + err.Error())
		}
		if err := os.WriteFile(dest, b, 0o644); err != nil {
			return note("export error: " + err.Error())
		}
		return note("exported jsonl: " + dest)
	case "clone":
		if m.engine == nil || m.engine.Opts.Session == nil {
			return note("no session to clone")
		}
		cwd, _ := os.Getwd()
		child, err := m.engine.Opts.Session.Fork(cwd, m.engine.Opts.AgentDir)
		if err != nil {
			return note("clone error: " + err.Error())
		}
		m.engine.AdoptSession(child)
		m.history = m.engine.History()
		return note("cloned session " + child.ID() + "\n" + child.File())
	case "fork":
		if m.engine == nil || m.engine.Opts.Session == nil {
			return note("no session to fork")
		}
		cwd, _ := os.Getwd()
		if cmd.Rest == "" {
			msgs := m.engine.Opts.Session.UserMessagesForForking()
			if len(msgs) == 0 {
				return note("no user messages to fork from")
			}
			var b strings.Builder
			b.WriteString("user messages ( /fork <entryId> ):\n")
			for _, row := range msgs {
				fmt.Fprintf(&b, "  %s  %s\n", row["entryId"][:min(8, len(row["entryId"]))], strings.ReplaceAll(row["text"], "\n", " "))
			}
			return note(b.String())
		}
		child, text, err := m.engine.Opts.Session.ForkFrom(cmd.Rest, cwd, m.engine.Opts.AgentDir, "before")
		if err != nil {
			return note("fork error: " + err.Error())
		}
		m.engine.AdoptSession(child)
		m.history = m.engine.History()
		if text != "" {
			m.textarea.SetValue(text)
		}
		return note("forked session " + child.ID() + "\n" + child.File())
	case "tree":
		if m.engine == nil || m.engine.Opts.Session == nil {
			return note("no session")
		}
		return note(m.engine.Opts.Session.FormatTree())
	case "resume":
		if m.engine == nil {
			return note("no engine")
		}
		cwd, _ := os.Getwd()
		if cmd.Rest == "" {
			list, err := session.Summaries(cwd, m.engine.Opts.AgentDir)
			if err != nil {
				return note("resume error: " + err.Error())
			}
			if len(list) == 0 {
				return note("no sessions in this directory")
			}
			var b strings.Builder
			b.WriteString("sessions ( /resume <id> ):\n")
			for _, s := range list {
				name := s.Name
				if name == "" {
					name = s.ID[:min(8, len(s.ID))]
				}
				fmt.Fprintf(&b, "  %s  %s\n", name, s.FirstMessage)
			}
			return note(b.String())
		}
		opened, err := session.FindByID(cwd, m.engine.Opts.AgentDir, cmd.Rest)
		if err != nil {
			opened, err = session.Open(cmd.Rest)
		}
		if err != nil {
			return note("resume error: " + err.Error())
		}
		m.engine.AdoptSession(opened)
		m.history = m.engine.History()
		return note("resumed " + opened.ID() + "\n" + opened.File())
	case "import":
		if cmd.Rest == "" {
			return note("usage: /import <path.jsonl>")
		}
		opened, err := session.Open(cmd.Rest)
		if err != nil {
			return note("import error: " + err.Error())
		}
		if m.engine != nil {
			m.engine.AdoptSession(opened)
			m.history = m.engine.History()
		}
		return note("imported " + opened.ID() + "\n" + opened.File())
	case "name":
		if m.engine == nil || m.engine.Opts.Session == nil {
			return note("no session")
		}
		if cmd.Rest != "" {
			m.engine.Opts.Session.SetName(cmd.Rest)
		}
		return note("session name = " + m.engine.Opts.Session.Name())
	case "login":
		return note("OAuth login is not available in pigo; run: pi auth login <provider>")
	case "logout":
		prov := cmd.Rest
		if prov == "" {
			prov = m.cfg.ResolvedProvider()
		}
		dir := config.DefaultConfigDir()
		if m.engine != nil {
			dir = m.engine.Opts.AgentDir
		}
		if err := auth.Delete(dir, prov); err != nil {
			return note("logout error: " + err.Error())
		}
		return note("removed stored key for " + prov)
	case "reload":
		if m.engine == nil {
			return note("reload requires a runtime engine")
		}
		m.engine.Reload()
		return note("reloaded skills and context files")
	case "copy":
		text := lastAssistant(m.history)
		if text == "" {
			return note("no assistant text to copy")
		}
		b64 := base64.StdEncoding.EncodeToString([]byte(text))
		osc := "\x1b]52;c;" + b64 + "\x07"
		return note(osc + "copied last assistant message")
	case "skill":
		if m.engine == nil {
			return note("no skills loaded")
		}
		name, args, _ := strings.Cut(cmd.Rest, " ")
		body, ok := skills.ExpandCommand(m.engine.Skills, name, args)
		if !ok {
			return note("unknown skill: " + name)
		}
		return m.startTurn(body)
	default:
		if m.engine != nil {
			if expanded, ok := prompt.ExpandTemplate("/"+cmd.Name+" "+cmd.Rest, m.engine.Templates); ok {
				return m.startTurn(expanded)
			}
			if body, ok := skills.ExpandCommand(m.engine.Skills, cmd.Name, cmd.Rest); ok {
				return m.startTurn(body)
			}
		}
		return note("/" + cmd.Name + " is not implemented")
	}
}

func (m Model) startTurn(text string) (tea.Model, tea.Cmd) {
	m.textarea.Reset()
	m.transcript = append(m.transcript, entry{role: "user", rendered: m.userStyle.Render("› you") + "\n" + indent(text)})
	m.history = append(m.history, ai.Message{Role: ai.RoleUser, Content: text})

	var stream *agent.Stream
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	if m.engine != nil {
		m.provider = m.engine.Provider
		// history already contains the user turn; RunPrompt appends user again, so pass without last
		hist := m.history[:len(m.history)-1]
		stream = m.engine.RunPrompt(ctx, hist, text)
	} else {
		sf, provider := ai.DefaultStreamFn()
		m.provider = provider
		reg := tools.Default()
		exec := agent.ToolFunc(func(ctx context.Context, c agent.ToolCall) (string, bool) {
			return reg.Execute(ctx, c.Name, c.Args)
		})
		reqCtx := ai.Context{Messages: append([]ai.Message(nil), m.history...), Tools: reg.AITools()}
		stream = agent.Run(ctx, sf, reqCtx, exec, agent.Config{Model: m.cfg.ResolvedModel()})
	}
	m.agentEvents = stream.Events()
	m.running = true
	m.streaming = ""
	m.streamingActive = false
	return m, waitForAgentEvent(m.agentEvents)
}

func waitForAgentEvent(ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return agentClosedMsg{}
		}
		ev, ok := <-ch
		if !ok {
			return agentClosedMsg{}
		}
		return agentEventMsg{ev}
	}
}

func (m *Model) applyAgentEvent(ev agent.Event) {
	switch ev.Type {
	case agent.EventMessageStart:
		m.streaming = ""
		m.streamingActive = true

	case agent.EventMessageUpdate:
		if ev.AIEvent != nil && ev.AIEvent.Type == ai.EventTextDelta {
			m.streaming += ev.AIEvent.Delta
		}

	case agent.EventMessageEnd:
		m.streamingActive = false
		m.streaming = ""
		if ev.Assistant != nil {
			if text := ev.Assistant.Text(); text != "" {
				m.transcript = append(m.transcript, entry{role: "assistant", rendered: m.renderMarkdown(text)})
				m.history = append(m.history, ai.Message{Role: ai.RoleAssistant, Content: text})
			}
		}

	case agent.EventToolStart:
		m.transcript = append(m.transcript, entry{
			role:     "tool",
			rendered: m.toolStyle.Render(fmt.Sprintf("⚙ %s %s", ev.ToolName, compactArgs(ev.Args))),
		})

	case agent.EventToolEnd:
		summary := firstLine(ev.Result)
		style := m.toolStyle
		mark := "→"
		if ev.IsError {
			style = m.errStyle
			mark = "✗"
		}
		m.transcript = append(m.transcript, entry{
			role:     "tool",
			rendered: style.Render(fmt.Sprintf("  %s %s", mark, summary)),
		})

	case agent.EventAgentEnd:
		m.running = false
		m.streamingActive = false
		if len(ev.Messages) > 0 {
			m.history = agent.MessagesFromTranscript(ev.Messages)
			if m.engine != nil {
				m.engine.PersistTranscript(ev.Messages)
			}
		}
	}
}

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return "bye\n"
	}

	var b strings.Builder
	b.WriteString(m.titleStyle.Render("pigo"))
	b.WriteString("  ")
	b.WriteString(m.metaStyle.Render(fmt.Sprintf("provider=%s  model=%s  theme=%s", m.providerLabel(), m.cfg.ResolvedModel(), m.cfg.Theme)))
	b.WriteString("\n\n")

	for _, e := range m.transcript {
		b.WriteString(e.rendered)
		b.WriteString("\n\n")
	}

	if m.streamingActive && m.streaming != "" {
		b.WriteString(m.streamStyle.Render(m.streaming))
		b.WriteString("\n\n")
	}
	if m.running {
		b.WriteString(m.metaStyle.Render("…working (Ctrl+C to interrupt)"))
		b.WriteString("\n\n")
	}

	b.WriteString(m.textarea.View())
	b.WriteString("\n")
	b.WriteString(m.footerStyle.Render("Enter send · Alt+Enter follow-up · Shift+Tab thinking · Ctrl+P model · /help · Ctrl+C exit"))
	return b.String()
}

func (m Model) providerLabel() string {
	if m.provider != "" {
		return m.provider
	}
	return "(none yet)"
}

func (m Model) renderMarkdown(s string) string {
	if m.glam == nil {
		return s
	}
	out, err := m.glam.Render(s)
	if err != nil {
		return s
	}
	return strings.TrimRight(out, "\n")
}

// Run starts the interactive TUI. It requires a TTY.
func Run(cfg config.Config) error {
	return RunEngine(cfg, nil)
}

// RunEngine starts the TUI with a preconfigured runtime engine (session, tools, skills).
func RunEngine(cfg config.Config, eng *runtime.Engine) error {
	m := New(cfg)
	m.engine = eng
	if eng != nil {
		m.provider = eng.Provider
		m.history = eng.History()
	}
	_, err := tea.NewProgram(m).Run()
	return err
}

func lastAssistant(msgs []ai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == ai.RoleAssistant {
			return msgs[i].Text()
		}
	}
	return ""
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = "  " + ln
	}
	return strings.Join(lines, "\n")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " …"
	}
	if len(s) > 200 {
		s = s[:200] + " …"
	}
	if s == "" {
		return "(no output)"
	}
	return s
}

func compactArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
