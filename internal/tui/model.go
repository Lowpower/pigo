package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lowpower/pigo/internal/config"
)

// maxEditorWidth caps the input width so the editor stays readable on wide terminals.
const maxEditorWidth = 100

// Model is the Phase 0 interactive editor: a multiline input plus an echo
// transcript. Keybindings follow pi (packages/tui/src/keybindings.ts):
// Enter submits, Shift+Enter / Ctrl+J insert a newline, Ctrl+C quits.
//
// The agent/LLM wiring lands in later phases; for now submitted input is only
// echoed back, so this slice needs no API access.
type Model struct {
	cfg      config.Config
	textarea textarea.Model
	history  []string
	width    int
	height   int
	quitting bool

	titleStyle  lipgloss.Style
	metaStyle   lipgloss.Style
	userStyle   lipgloss.Style
	bodyStyle   lipgloss.Style
	footerStyle lipgloss.Style
}

// New builds the Phase 0 editor model from the resolved config.
func New(cfg config.Config) Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message… (Enter to submit, Shift+Enter for newline)"
	ta.Prompt = "│ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	// pi binds newline to shift+enter / ctrl+j; Enter is handled by us as submit,
	// so it must not also insert a newline in the textarea.
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	ta.Focus()

	return Model{
		cfg:         cfg,
		textarea:    ta,
		titleStyle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")),
		metaStyle:   lipgloss.NewStyle().Faint(true),
		userStyle:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")),
		bodyStyle:   lipgloss.NewStyle().PaddingLeft(2),
		footerStyle: lipgloss.NewStyle().Faint(true),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textarea.SetWidth(min(msg.Width, maxEditorWidth))
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			return m.submit()
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

// submit records the trimmed input as an echoed message and clears the editor.
// "/quit" and "/exit" end the program (a nod to pi's slash commands).
func (m Model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return m, nil
	}
	if text == "/quit" || text == "/exit" {
		m.quitting = true
		return m, tea.Quit
	}
	m.history = append(m.history, text)
	m.textarea.Reset()
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return "bye\n"
	}

	var b strings.Builder
	b.WriteString(m.titleStyle.Render("pigo"))
	b.WriteString("  ")
	b.WriteString(m.metaStyle.Render(fmt.Sprintf("provider=%s  model=%s  theme=%s",
		m.cfg.Provider, m.cfg.Model, m.cfg.Theme)))
	b.WriteString("\n\n")

	if len(m.history) == 0 {
		b.WriteString(m.metaStyle.Render(
			"Phase 0 editor — input is echoed back (no agent yet)."))
		b.WriteString("\n\n")
	} else {
		for _, entry := range m.history {
			b.WriteString(m.userStyle.Render("› you"))
			b.WriteString("\n")
			b.WriteString(m.bodyStyle.Render(entry))
			b.WriteString("\n\n")
		}
	}

	b.WriteString(m.textarea.View())
	b.WriteString("\n")
	b.WriteString(m.footerStyle.Render(
		"Enter submit · Shift+Enter/Ctrl+J newline · /quit or Ctrl+C exit"))
	return b.String()
}

// History returns the submitted messages (used by tests).
func (m Model) History() []string {
	return m.history
}

// Run starts the Phase 0 interactive editor. It requires a TTY.
func Run(cfg config.Config) error {
	_, err := tea.NewProgram(New(cfg)).Run()
	return err
}
