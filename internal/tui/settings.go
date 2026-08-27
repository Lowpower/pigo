package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/theme"
)

type settingsPicker struct {
	listPicker
}

func (m Model) settingsActive() bool { return m.settings.active }

func (m Model) openSettings() (tea.Model, tea.Cmd) {
	m.settings = settingsPicker{listPicker: listPicker{
		title:        "Settings",
		hint:         "Enter cycle · Esc close",
		detailPrefix: "",
		active:       true,
	}}
	if dir := m.settingsDir(); dir != "" {
		m.settings.hint += " · " + filepath.ToSlash(dir) + "/settings.json"
	}
	m.refreshSettingsItems()
	return m, nil
}

func (m Model) settingsDir() string {
	if m.engine != nil && m.engine.Opts.AgentDir != "" {
		return m.engine.Opts.AgentDir
	}
	return config.DefaultConfigDir()
}

func (m *Model) refreshSettingsItems() {
	keep := ""
	if it, ok := m.settings.current(); ok {
		keep = it.ID
	}
	type row struct {
		id, label, desc, value string
	}
	rows := []row{
		{"autocompact", "Auto-compact", "Automatically compact context when it gets too large", boolText(m.cfg.CompactionEnabled())},
		{"steering-mode", "Steering mode", "Enter while streaming queues steering messages", oneOrAll(m.cfg.SteeringMode)},
		{"follow-up-mode", "Follow-up mode", "Queued follow-ups when the agent is idle", oneOrAll(m.cfg.FollowUpMode)},
		{"mermaid-rendering", "Mermaid diagrams", "Render Mermaid code blocks as Unicode diagrams", m.cfg.MermaidMode()},
		{"show-images", "Show images", "Inline tool-result images when the terminal supports it", boolText(m.cfg.ShowImages())},
		{"block-images", "Block images", "Omit images from requests sent to the model", boolText(m.cfg.BlockImages())},
		{"default-project-trust", "Default project trust", "When a project has local resources and no saved decision", m.cfg.ProjectTrustDefault()},
		{"double-escape-action", "Double-escape action", "Action when pressing Escape twice with empty editor", m.cfg.DoubleEscape()},
		{"tree-filter-mode", "Tree filter mode", "Default filter when opening /tree", m.cfg.TreeFilter()},
		{"theme", "Theme", "Colour theme", m.theme.Name},
		{"thinking", "Thinking level", "Reasoning level for the current session", thinkingValue(m.cfg.Thinking)},
		{"collapse-changelog", "Collapse changelog", "Show condensed changelog after updates", boolText(m.cfg.CollapsedChangelog())},
		{"install-telemetry", "Install telemetry", "Send an anonymous version/update ping after changelog-detected updates", boolText(m.cfg.InstallTelemetryEnabled())},
		{"tui-mode", "TUI mode", "Interface layout; fullscreen uses the alternate screen", m.cfg.TuiMode()},
		{"fullscreen-exit-output", "Fullscreen exit output", "Print the transcript or a resume hint when leaving fullscreen", m.cfg.FullscreenExit()},
	}
	items := make([]pickerItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, pickerItem{ID: r.id, Label: r.label, Meta: r.value, Aux: r.desc})
	}
	m.settings.setItems(items)
	if keep == "" {
		return
	}
	for i, it := range m.settings.filtered {
		if it.ID == keep {
			m.settings.selected = i
			return
		}
	}
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func oneOrAll(s string) string {
	if s == "all" {
		return "all"
	}
	return "one-at-a-time"
}

func thinkingValue(s string) string {
	if models.IsThinkingLevel(s) {
		return s
	}
	return "off"
}

func nextChoice(cur string, values []string) string {
	if len(values) == 0 {
		return cur
	}
	for i, v := range values {
		if v == cur {
			return values[(i+1)%len(values)]
		}
	}
	return values[0]
}

func (m Model) settingChoices(id string) []string {
	switch id {
	case "autocompact", "show-images", "block-images", "collapse-changelog", "install-telemetry":
		return []string{"true", "false"}
	case "steering-mode", "follow-up-mode":
		return []string{"one-at-a-time", "all"}
	case "mermaid-rendering":
		return []string{"off", "final", "streaming"}
	case "double-escape-action":
		return []string{"tree", "fork", "none"}
	case "default-project-trust":
		return []string{"ask", "always", "never"}
	case "tree-filter-mode":
		return []string{"default", "no-tools", "user-only", "labeled-only", "all"}
	case "theme":
		names := theme.NamesWith(m.themeOpts(""))
		if len(names) == 0 {
			return []string{"default", "dark", "light"}
		}
		return names
	case "thinking":
		return append([]string{}, models.ThinkingLevels...)
	case "tui-mode":
		return []string{"regular", "fullscreen"}
	case "fullscreen-exit-output":
		return []string{"transcript", "resume-hint"}
	default:
		return nil
	}
}

func (m Model) settingCurrent(id string) string {
	switch id {
	case "autocompact":
		return boolText(m.cfg.CompactionEnabled())
	case "steering-mode":
		return oneOrAll(m.cfg.SteeringMode)
	case "follow-up-mode":
		return oneOrAll(m.cfg.FollowUpMode)
	case "mermaid-rendering":
		return m.cfg.MermaidMode()
	case "show-images":
		return boolText(m.cfg.ShowImages())
	case "block-images":
		return boolText(m.cfg.BlockImages())
	case "default-project-trust":
		return m.cfg.ProjectTrustDefault()
	case "double-escape-action":
		return m.cfg.DoubleEscape()
	case "tree-filter-mode":
		return m.cfg.TreeFilter()
	case "theme":
		if m.theme.Name != "" {
			return m.theme.Name
		}
		return m.cfg.Theme
	case "thinking":
		return thinkingValue(m.cfg.Thinking)
	case "collapse-changelog":
		return boolText(m.cfg.CollapsedChangelog())
	case "install-telemetry":
		return boolText(m.cfg.InstallTelemetryEnabled())
	case "tui-mode":
		return m.cfg.TuiMode()
	case "fullscreen-exit-output":
		return m.cfg.FullscreenExit()
	default:
		return ""
	}
}

func (m *Model) applySetting(id, value string) tea.Cmd {
	var cmd tea.Cmd
	switch id {
	case "autocompact":
		on := value == "true"
		m.cfg.CompactionOn = &on
	case "steering-mode":
		m.cfg.SteeringMode = value
	case "follow-up-mode":
		m.cfg.FollowUpMode = value
	case "mermaid-rendering":
		m.cfg.Markdown.Mermaid = value
	case "show-images":
		on := value == "true"
		m.cfg.Terminal.ShowImages = &on
	case "block-images":
		on := value == "true"
		m.cfg.Images.BlockImages = &on
	case "default-project-trust":
		m.cfg.DefaultProjectTrust = value
	case "double-escape-action":
		m.cfg.DoubleEscapeAction = value
	case "tree-filter-mode":
		m.cfg.TreeFilterMode = value
	case "theme":
		m.cfg.Theme = value
		m.applyTheme(theme.LoadWith(m.themeOpts(value)))
	case "thinking":
		m.cfg.Thinking = value
	case "collapse-changelog":
		on := value == "true"
		m.cfg.CollapseChangelog = &on
	case "install-telemetry":
		on := value == "true"
		m.cfg.EnableInstallTelemetry = &on
	case "tui-mode":
		m.cfg.TUIMode = value
		m.altScreen = value == "fullscreen"
		if m.altScreen {
			cmd = tea.EnterAltScreen
		} else {
			cmd = tea.ExitAltScreen
		}
	case "fullscreen-exit-output":
		m.cfg.FullscreenExitOutput = value
	}
	m.persistSettings()
	return cmd
}

func (m *Model) persistSettings() {
	if m.engine != nil {
		m.engine.Opts.Config = m.cfg
		if m.engine.Opts.AgentDir != "" {
			_ = config.Save(m.engine.Opts.AgentDir, m.cfg)
			return
		}
	}
	_ = config.Save(config.DefaultConfigDir(), m.cfg)
}

func (m Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	act := m.settings.handleKey(msg, m.keys)
	switch act {
	case "cancel":
		m.settings = settingsPicker{}
		return m, nil
	case "confirm":
		it, ok := m.settings.current()
		if !ok {
			return m, nil
		}
		next := nextChoice(m.settingCurrent(it.ID), m.settingChoices(it.ID))
		cmd := m.applySetting(it.ID, next)
		m.refreshSettingsItems()
		return m, cmd
	}
	return m, nil
}
