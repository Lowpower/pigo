package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
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

// Model is the Phase 5 interactive TUI: it drives the agent loop (internal/agent)
// with real tools (internal/tools) and a provider (internal/ai), streaming the
// assistant response to screen live (plain text during the turn), then rendering
// it as markdown via glamour once the turn ends. Keybindings follow pi.
type Model struct {
	cfg      config.Config
	textarea textarea.Model

	transcript []entry
	history    []ai.Message // raw user/assistant messages carried across turns

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

	m := Model{
		cfg:         cfg,
		textarea:    ta,
		titleStyle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")),
		metaStyle:   lipgloss.NewStyle().Faint(true),
		userStyle:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")),
		toolStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("178")),
		errStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		streamStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		footerStyle: lipgloss.NewStyle().Faint(true),
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
		case "enter":
			return m.submit()
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
		return m, nil
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	if m.running {
		return m, nil // ignore submits while a turn is in flight
	}
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return m, nil
	}
	if text == "/quit" || text == "/exit" {
		m.quitting = true
		return m, tea.Quit
	}
	m.textarea.Reset()
	m.transcript = append(m.transcript, entry{role: "user", rendered: m.userStyle.Render("› you") + "\n" + indent(text)})
	m.history = append(m.history, ai.Message{Role: ai.RoleUser, Content: text})

	sf, provider := ai.DefaultStreamFn()
	m.provider = provider
	reg := tools.Default()
	exec := agent.ToolFunc(func(ctx context.Context, c agent.ToolCall) (string, bool) {
		return reg.Execute(ctx, c.Name, c.Args)
	})

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	reqCtx := ai.Context{Messages: append([]ai.Message(nil), m.history...), Tools: reg.AITools()}
	stream := agent.Run(ctx, sf, reqCtx, exec, agent.Config{Model: m.cfg.Model})
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
	b.WriteString(m.metaStyle.Render(fmt.Sprintf("provider=%s  model=%s", m.providerLabel(), m.cfg.Model)))
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
	b.WriteString(m.footerStyle.Render("Enter send · Shift+Enter/Ctrl+J newline · /quit or Ctrl+C exit"))
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
	_, err := tea.NewProgram(New(cfg)).Run()
	return err
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
