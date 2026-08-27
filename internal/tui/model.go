package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/keys"
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
	thinking string // raw thinking block (Ctrl+T)
	toolOut  string // raw tool result (Ctrl+O)
	isError  bool
}

// agentEventMsg / agentClosedMsg carry agent-loop events into the bubbletea loop.
type agentEventMsg struct{ ev agent.Event }
type agentClosedMsg struct{}

type queuedPrompt struct {
	text   string
	images []ai.ImageContent
}

// Model is the interactive TUI: it drives the agent loop (internal/agent)
// with real tools (internal/tools) and a provider (internal/ai), streaming the
// assistant response to screen live (plain text during the turn), then rendering
// it as markdown via glamour once the turn ends.
type Model struct {
	cfg    config.Config
	engine *runtime.Engine
	theme  theme.Theme
	editor promptEditor

	transcript []entry
	history    []ai.Message // raw user/assistant messages carried across turns
	queued     []queuedPrompt

	streaming         string // in-progress assistant text (plain)
	streamingThinking string
	streamingActive   bool
	running           bool
	provider          string
	hideThinking      bool
	toolsExpanded     bool
	usage             ai.Usage
	gitCwd            string
	gitBranch         string

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

	keys        *keys.Manager
	models      modelPicker
	sessions    sessionPicker
	complete    completer
	completeDir string
	bashCancel  context.CancelFunc
	bashRunning bool
	lastClear   time.Time
	login       loginState

	overlay       overlayKind
	tree          treeOverlay
	fork          forkOverlay
	lastEscape    time.Time
	summaryCancel context.CancelFunc
	pendingNav    *pendingNav
	clipOSC       string
	imgProto      string
}

// New builds the interactive model from the resolved config.
func New(cfg config.Config) Model {
	m := Model{
		cfg:      cfg,
		editor:   newPromptEditor(),
		keys:     keys.NewManager(config.DefaultConfigDir()),
		imgProto: detectImageProtocol(os.Getenv),
	}
	m.glam = newRenderer(80)
	m.applyTheme(theme.Load(cfg.Theme, "", ""))
	m.refreshGit()
	return m
}

func (m *Model) applyTheme(th theme.Theme) {
	m.theme = th
	m.titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(th.Accent))
	m.metaStyle = lipgloss.NewStyle().Faint(true)
	m.userStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(th.User))
	m.toolStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(th.Tool))
	m.errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(th.Error))
	m.streamStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(th.Assistant))
	m.footerStyle = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(th.Muted))
}

func (m Model) themeOpts(name string) theme.LoadOptions {
	opt := theme.LoadOptions{Name: name}
	if m.engine != nil {
		opt.Cwd = m.engine.Opts.Cwd
		opt.AgentDir = m.engine.Opts.AgentDir
		opt.Extra = m.engine.Opts.ThemePaths
		opt.NoDiscovery = m.engine.Opts.NoThemes
	}
	return opt
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
		m.editor.SetWidth(min(msg.Width, maxEditorWidth))
		m.glam = newRenderer(min(msg.Width, maxEditorWidth))
		return m, nil

	case externalEditorDoneMsg:
		if msg.ok {
			m.editor.SetValue(msg.content)
		}
		return m, nil

	case bashDoneMsg:
		return m.handleBashDone(msg)

	case tea.KeyMsg:
		if m.sessionPickerActive() {
			return m.handleSessionPickerKey(msg)
		}
		if m.overlay != overlayNone {
			switch m.overlay {
			case overlayFork:
				return m.handleForkKey(msg)
			default:
				return m.handleTreeKey(msg)
			}
		}
		if m.modelPickerActive() {
			return m.handleModelPickerKey(msg)
		}
		if m.loginActive() {
			return m.handleLoginKey(msg)
		}
		if m.complete.active {
			if m.keyIs(msg, "tui.select.up") || m.keyIs(msg, "tui.editor.cursorUp") {
				m.complete.move(-1)
				return m, nil
			}
			if m.keyIs(msg, "tui.select.down") || m.keyIs(msg, "tui.editor.cursorDown") {
				m.complete.move(1)
				return m, nil
			}
			if m.keyIs(msg, "tui.select.confirm") || m.keyIs(msg, "tui.input.submit") || m.keyIs(msg, "tui.input.tab") {
				return m.applyCompletion()
			}
			if m.keyIs(msg, "tui.select.cancel") || m.keyIs(msg, "app.interrupt") {
				m.complete.hide()
				return m, nil
			}
		}
		if m.editor.jump != "" {
			if m.keyIs(msg, "tui.editor.jumpForward") || m.keyIs(msg, "tui.editor.jumpBackward") {
				m.editor.jump = ""
				return m, nil
			}
			if ch, ok := printableJump(msg); ok {
				dir := m.editor.jump
				m.editor.jump = ""
				m.editor.jumpTo(ch, dir)
				return m, nil
			}
			m.editor.jump = ""
		}
		if m.keyIs(msg, "app.clear") {
			return m.handleClear()
		}
		if m.keyIs(msg, "app.exit") && strings.TrimSpace(m.editor.Value()) == "" {
			m.quitting = true
			return m, tea.Quit
		}
		if m.keyIs(msg, "app.interrupt") {
			if m.running && m.cancel != nil {
				m.cancel()
				return m, nil
			}
			if m.bashRunning && m.bashCancel != nil {
				m.bashCancel()
				return m, nil
			}
			return m.handleIdleEscape()
		}
		if m.keyIs(msg, "tui.input.tab") {
			m.refreshComplete(true)
			if m.complete.active && len(m.complete.items) == 1 {
				m.complete.sel = 0
				return m.applyCompletion()
			}
			return m, nil
		}
		if m.keyIs(msg, "tui.input.submit") {
			return m.submit()
		}
		if m.keyIs(msg, "app.message.followUp") {
			return m.queueFollowUp()
		}
		if m.keyIs(msg, "app.model.cycleForward") {
			return m.cycleModel(false)
		}
		if m.keyIs(msg, "app.model.cycleBackward") {
			return m.cycleModel(true)
		}
		if m.keyIs(msg, "app.thinking.cycle") {
			return m.cycleThinking()
		}
		if m.keyIs(msg, "app.thinking.toggle") {
			m.hideThinking = !m.hideThinking
			state := "visible"
			if m.hideThinking {
				state = "hidden"
			}
			m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("thinking blocks: " + state)})
			return m, nil
		}
		if m.keyIs(msg, "app.tools.expand") {
			m.toolsExpanded = !m.toolsExpanded
			state := "collapsed"
			if m.toolsExpanded {
				state = "expanded"
			}
			m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("tool output: " + state)})
			return m, nil
		}
		if m.keyIs(msg, "app.model.select") {
			return m.openModelPicker("")
		}
		if m.keyIs(msg, "app.session.tree") {
			return m.openTree()
		}
		if m.keyIs(msg, "app.session.fork") {
			return m.openFork()
		}
		if m.keyIs(msg, "app.session.resume") {
			return m.openSessionPicker()
		}
		if m.keyIs(msg, "app.editor.external") {
			return m.openExternalEditor()
		}
		if m.keyIs(msg, "app.clipboard.pasteImage") {
			m.editor.pasteClipboard()
			return m, nil
		}
		if m.editor.handle(msg, m.keys) {
			m.refreshComplete(false)
			return m, nil
		}

	case loginDoneMsg, loginEventMsg, loginPromptMsg:
		return m.handleLoginMsg(msg)

	case agentEventMsg:
		m.applyAgentEvent(msg.ev)
		return m, waitForAgentEvent(m.agentEvents)

	case agentClosedMsg:
		m.running = false
		m.streamingActive = false
		m.streaming = ""
		m.agentEvents = nil
		m.cancel = nil
		if m.pendingNav != nil {
			nav := *m.pendingNav
			m.pendingNav = nil
			return m.applyTreeNav(nav.target, nav.summarize, nav.custom, nav.replace)
		}
		if len(m.queued) > 0 {
			next := m.queued[0]
			m.queued = m.queued[1:]
			return m.startTurn(next.text, next.images)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.editor.ta, cmd = m.editor.ta.Update(msg)
	if key, ok := msg.(tea.KeyMsg); ok && m.keys != nil {
		m.editor.afterTextareaKey(key, m.keys)
	}
	m.refreshComplete(false)
	return m, cmd
}

func (m Model) queueFollowUp() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.editor.Expanded())
	if text == "" {
		return m, nil
	}
	images := extractImages(text)
	m.editor.Reset()
	if !m.running {
		return m.startTurn(text, images)
	}
	if m.engine != nil {
		m.engine.PushFollowImages(text, images)
	} else {
		m.queued = append(m.queued, queuedPrompt{text: text, images: images})
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
	text := strings.TrimSpace(m.editor.Expanded())
	if text == "" {
		return m, nil
	}
	images := extractImages(text)
	if command, exclude, ok := parseBang(text); ok {
		m.editor.AddHistory(text)
		m.editor.Reset()
		m.complete.hide()
		return m.startBash(command, exclude)
	}
	if cmd, ok := slash.Parse(text); ok {
		return m.handleSlash(cmd)
	}
	if m.running {
		m.editor.Reset()
		// Enter while streaming steers the current loop; leftover lines
		// after the turn are follow-ups (drained on agentClosedMsg).
		if m.engine != nil {
			m.engine.PushSteerImages(text, images)
			m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("steering: " + text)})
			return m, nil
		}
		m.queued = append(m.queued, queuedPrompt{text: text, images: images})
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("queued: " + text)})
		return m, nil
	}
	return m.startTurn(text, images)
}

func (m Model) handleSlash(cmd slash.Command) (tea.Model, tea.Cmd) {
	m.editor.Reset()
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
		if m.keys != nil {
			return note(m.keys.HotkeysText())
		}
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
		return m.handleModelCommand(cmd.Rest)
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
			m.applyTheme(theme.LoadWith(m.themeOpts(cmd.Rest)))
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
		m.reloadFromSession()
		return note("cloned session " + child.ID() + "\n" + child.File())
	case "fork":
		if m.engine == nil || m.engine.Opts.Session == nil {
			return note("no session to fork")
		}
		cwd, _ := os.Getwd()
		if cmd.Rest == "" {
			return m.openFork()
		}
		child, text, err := m.engine.Opts.Session.ForkFrom(cmd.Rest, cwd, m.engine.Opts.AgentDir, "before")
		if err != nil {
			return note("fork error: " + err.Error())
		}
		m.engine.AdoptSession(child)
		m.reloadFromSession()
		if text != "" {
			m.editor.SetValue(text)
		}
		return note("forked session " + child.ID() + "\n" + child.File())
	case "tree":
		return m.openTree()
	case "resume":
		return m.handleResumeCommand(cmd.Rest)
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
			m.reloadFromSession()
		}
		return note("imported " + opened.ID() + "\n" + opened.File())
	case "name":
		if m.engine == nil || m.engine.Opts.Session == nil {
			return note("no session")
		}
		if cmd.Rest != "" {
			m.engine.Opts.Session.SetName(cmd.Rest)
			_ = session.UpdateHeader(m.engine.Opts.Session.File(), func(h *session.Header) { h.Name = cmd.Rest })
		}
		return note("session name = " + m.engine.Opts.Session.Name())
	case "login":
		return m.startLogin(cmd.Rest)
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
		if m.keys != nil {
			m.keys.Reload()
		}
		return note("reloaded keybindings, skills, and context files")
	case "copy":
		text := lastAssistant(m.history)
		if text == "" {
			return note("no assistant text to copy")
		}
		return note(osc52(text) + "copied last assistant message")
	case "skill":
		if m.engine == nil {
			return note("no skills loaded")
		}
		name, args, _ := strings.Cut(cmd.Rest, " ")
		body, ok := skills.ExpandCommand(m.engine.Skills, name, args)
		if !ok {
			return note("unknown skill: " + name)
		}
		return m.startTurn(body, nil)
	default:
		if m.engine != nil {
			if expanded, ok := prompt.ExpandTemplate("/"+cmd.Name+" "+cmd.Rest, m.engine.Templates); ok {
				return m.startTurn(expanded, nil)
			}
			if body, ok := skills.ExpandCommand(m.engine.Skills, cmd.Name, cmd.Rest); ok {
				return m.startTurn(body, nil)
			}
		}
		return note("/" + cmd.Name + " is not implemented")
	}
}

func (m Model) startTurn(text string, images []ai.ImageContent) (tea.Model, tea.Cmd) {
	m.editor.AddHistory(text)
	m.editor.Reset()
	m.transcript = append(m.transcript, entry{role: "user", rendered: m.userStyle.Render("› you") + "\n" + indent(text)})
	m.history = append(m.history, ai.Message{Role: ai.RoleUser, Content: text, Images: images})

	var stream *agent.Stream
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	if m.engine != nil {
		m.provider = m.engine.Provider
		// history already contains the user turn; RunPrompt appends user again, so pass without last
		hist := m.history[:len(m.history)-1]
		stream = m.engine.RunPrompt(ctx, hist, text, images)
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
	m.streamingThinking = ""
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
		m.streamingThinking = ""
		m.streamingActive = true

	case agent.EventMessageUpdate:
		if ev.AIEvent != nil && ev.AIEvent.Type == ai.EventTextDelta {
			m.streaming += ev.AIEvent.Delta
		}
		if ev.AIEvent != nil && ev.AIEvent.Type == ai.EventThinkingDelta {
			m.streamingThinking += ev.AIEvent.Delta
		}

	case agent.EventMessageEnd:
		m.streamingActive = false
		m.streaming = ""
		m.streamingThinking = ""
		if ev.Assistant != nil {
			addUsage(&m.usage, ev.Assistant.Usage)
			for _, c := range ev.Assistant.Content {
				if c != nil && c.Type == ai.KindThinking && strings.TrimSpace(c.Thinking) != "" {
					m.transcript = append(m.transcript, entry{
						role:     "thinking",
						thinking: c.Thinking,
						rendered: m.metaStyle.Render(c.Thinking),
					})
				}
			}
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
		style := m.toolStyle
		mark := "→"
		if ev.IsError {
			style = m.errStyle
			mark = "✗"
		}
		m.transcript = append(m.transcript, entry{
			role:     "tool",
			toolOut:  ev.Result,
			isError:  ev.IsError,
			rendered: style.Render(fmt.Sprintf("  %s %s", mark, firstLine(ev.Result))),
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
	if m.sessionPickerActive() {
		return m.sessions.view()
	}
	if m.modelPickerActive() {
		return m.models.view()
	}
	if m.loginActive() {
		return m.loginView()
	}
	if m.overlay == overlayTree || m.overlay == overlayTreeLabel || m.overlay == overlayTreeSummary || m.overlay == overlayTreeCustom {
		return m.withClip(m.treeView())
	}
	if m.overlay == overlayFork {
		return m.withClip(m.forkView())
	}

	var b strings.Builder
	b.WriteString(m.titleStyle.Render("pigo"))
	b.WriteString("  ")
	b.WriteString(m.metaStyle.Render(fmt.Sprintf("provider=%s  model=%s  theme=%s", m.providerLabel(), m.cfg.ResolvedModel(), m.cfg.Theme)))
	b.WriteString("\n\n")

	for _, e := range m.transcript {
		switch e.role {
		case "thinking":
			if m.hideThinking {
				b.WriteString(m.metaStyle.Render("Thinking…"))
			} else if e.rendered != "" {
				b.WriteString(e.rendered)
			} else {
				b.WriteString(m.metaStyle.Render(e.thinking))
			}
		case "tool":
			if e.toolOut != "" {
				style := m.toolStyle
				mark := "→"
				if e.isError {
					style = m.errStyle
					mark = "✗"
				}
				text, imgs := splitToolImages(e.toolOut)
				if text != "" || len(imgs) == 0 {
					b.WriteString(style.Render(fmt.Sprintf("  %s %s", mark, toolResultBody(text, m.toolsExpanded))))
				} else {
					b.WriteString(style.Render("  " + mark))
				}
				for _, img := range imgs {
					b.WriteByte('\n')
					out := m.renderInlineImage(img)
					if strings.HasPrefix(out, "\x1b") {
						b.WriteString(out)
					} else {
						b.WriteString(style.Render(out))
					}
				}
			} else {
				b.WriteString(e.rendered)
			}
		default:
			b.WriteString(e.rendered)
		}
		b.WriteString("\n\n")
	}

	if m.streamingThinking != "" {
		if m.hideThinking {
			b.WriteString(m.metaStyle.Render("Thinking…"))
		} else {
			b.WriteString(m.metaStyle.Render(m.streamingThinking))
		}
		b.WriteString("\n\n")
	}
	if m.streamingActive && m.streaming != "" {
		b.WriteString(m.streamStyle.Render(transformMermaid(m.streaming, m.mermaidOpts(true))))
		b.WriteString("\n\n")
	}
	if m.running {
		b.WriteString(m.metaStyle.Render("…working (Ctrl+C to interrupt)"))
		b.WriteString("\n\n")
	}
	if m.bashRunning {
		b.WriteString(m.metaStyle.Render("…bash (Esc to cancel)"))
		b.WriteString("\n\n")
	}

	b.WriteString(m.editor.View())
	b.WriteString("\n")
	if m.complete.active {
		b.WriteString(m.complete.view())
	}
	b.WriteString(m.footerStyle.Render(m.footerText()))
	return m.withClip(b.String())
}

type pendingNav struct {
	target    string
	summarize bool
	custom    string
	replace   bool
}

func (m Model) turnBusy() bool {
	return m.running || m.agentEvents != nil
}

func (m *Model) restoreQueuedToEditor() {
	var parts []string
	if m.engine != nil {
		s, f := m.engine.TakeQueues()
		parts = append(parts, s...)
		parts = append(parts, f...)
	}
	for _, q := range m.queued {
		if strings.TrimSpace(q.text) != "" {
			parts = append(parts, q.text)
		}
	}
	m.queued = nil
	var keep []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			keep = append(keep, p)
		}
	}
	if len(keep) == 0 {
		return
	}
	cur := m.editor.Value()
	if strings.TrimSpace(cur) != "" {
		keep = append(keep, cur)
	}
	m.editor.SetValue(strings.Join(keep, "\n\n"))
}

func osc52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}

func (m Model) withClip(s string) string {
	if m.clipOSC == "" {
		return s
	}
	return m.clipOSC + s
}

func (m Model) providerLabel() string {
	if m.provider != "" {
		return m.provider
	}
	return "(none yet)"
}

func (m Model) mermaidWidth() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	if w > maxEditorWidth {
		w = maxEditorWidth
	}
	wrap := w - 2
	if wrap < 20 {
		wrap = 20
	}
	return wrap
}

func (m Model) mermaidOpts(streaming bool) mermaidOpts {
	return mermaidOpts{mode: m.cfg.MermaidMode(), width: m.mermaidWidth(), streaming: streaming}
}

func (m Model) renderMarkdown(s string) string {
	s = transformMermaid(s, m.mermaidOpts(false))
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
	return runEngine(cfg, eng, false)
}

// RunEngineResumePicker starts the TUI and opens the session picker (--resume).
func RunEngineResumePicker(cfg config.Config, eng *runtime.Engine) error {
	return runEngine(cfg, eng, true)
}

func runEngine(cfg config.Config, eng *runtime.Engine, openResume bool) error {
	m := New(cfg)
	m.engine = eng
	if eng != nil {
		m.provider = eng.Provider
		m.reloadFromSession()
		m.keys = keys.NewManager(eng.Opts.AgentDir)
		m.applyTheme(theme.LoadWith(m.themeOpts(cfg.Theme)))
		m.refreshGit()
	}
	if openResume {
		next, _ := m.openSessionPicker()
		m = next.(Model)
	}
	_, err := tea.NewProgram(m).Run()
	return err
}

func (m Model) keyIs(msg tea.KeyMsg, action string) bool {
	if m.keys == nil {
		return false
	}
	return m.keys.Matches(msg.String(), action)
}

func (m Model) handleClear() (tea.Model, tea.Cmd) {
	now := time.Now()
	if !m.lastClear.IsZero() && now.Sub(m.lastClear) < 500*time.Millisecond {
		if m.cancel != nil {
			m.cancel()
		}
		m.quitting = true
		return m, tea.Quit
	}
	m.lastClear = now
	m.editor.Reset()
	m.complete.hide()
	return m, nil
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
