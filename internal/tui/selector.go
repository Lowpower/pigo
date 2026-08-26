package tui

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lowpower/pigo/internal/pkgmgr"
)

type configRowKind int

const (
	configRowGroup configRowKind = iota
	configRowSubgroup
	configRowItem
)

type configRow struct {
	kind     configRowKind
	label    string
	resource pkgmgr.Resource
}

// ConfigSelector is the `pigo config` resource enable/disable TUI.
type ConfigSelector struct {
	mgr          *pkgmgr.Manager
	writeProject bool
	projectOK    bool
	global       []pkgmgr.Resource
	project      []pkgmgr.Resource
	inherited    map[string]bool
	rows         []configRow
	cursor       int
	width        int
	height       int
	err          error
	quit         bool

	titleStyle  lipgloss.Style
	hintStyle   lipgloss.Style
	groupStyle  lipgloss.Style
	subStyle    lipgloss.Style
	onStyle     lipgloss.Style
	offStyle    lipgloss.Style
	plusStyle   lipgloss.Style
	minusStyle  lipgloss.Style
	dimStyle    lipgloss.Style
	cursorStyle lipgloss.Style
	mutedStyle  lipgloss.Style
}

func resourceKey(r pkgmgr.Resource) string {
	return r.Type + "\x00" + r.Path
}

func formatBaseDir(baseDir string) string {
	home, err := os.UserHomeDir()
	display := filepath.ToSlash(baseDir)
	if err == nil && home != "" {
		homeSlash := filepath.ToSlash(home)
		if display == homeSlash {
			display = "~"
		} else if strings.HasPrefix(display, homeSlash+"/") {
			display = "~" + display[len(homeSlash):]
		}
	}
	if !strings.HasSuffix(display, "/") {
		display += "/"
	}
	return display
}

func groupLabel(r pkgmgr.Resource, agentDir string) string {
	if r.Origin == "package" {
		return r.Source + " (" + r.Scope + ")"
	}
	if r.Source == "auto" {
		base := r.BaseDir
		if base == "" {
			base = agentDir
		}
		if r.Scope == "user" {
			return "User (" + formatBaseDir(base) + ")"
		}
		return "Project (" + formatBaseDir(base) + ")"
	}
	if r.Scope == "user" {
		return "User settings"
	}
	return "Project settings"
}

func displayName(r pkgmgr.Resource) string {
	fileName := filepath.Base(r.Path)
	parent := filepath.Base(filepath.Dir(r.Path))
	if r.Type == pkgmgr.KindExtensions && parent != "extensions" {
		return parent + "/" + fileName
	}
	if r.Type == pkgmgr.KindSkills && fileName == "SKILL.md" {
		return parent
	}
	return fileName
}

func typeLabel(kind string) string {
	switch kind {
	case pkgmgr.KindExtensions:
		return "Extensions"
	case pkgmgr.KindSkills:
		return "Skills"
	case pkgmgr.KindPrompts:
		return "Prompts"
	case pkgmgr.KindThemes:
		return "Themes"
	default:
		return kind
	}
}

type configGroup struct {
	key       string
	label     string
	subgroups []configSubgroup
}

type configSubgroup struct {
	kind  string
	label string
	items []pkgmgr.Resource
}

func buildConfigGroups(rs []pkgmgr.Resource, agentDir string) []configGroup {
	var groups []configGroup
	index := map[string]int{}
	for _, r := range rs {
		gk := r.Origin + ":" + r.Scope + ":" + r.Source + ":" + r.BaseDir
		gi, ok := index[gk]
		if !ok {
			gi = len(groups)
			index[gk] = gi
			groups = append(groups, configGroup{key: gk, label: groupLabel(r, agentDir)})
		}
		g := &groups[gi]
		var sg *configSubgroup
		for i := range g.subgroups {
			if g.subgroups[i].kind == r.Type {
				sg = &g.subgroups[i]
				break
			}
		}
		if sg == nil {
			g.subgroups = append(g.subgroups, configSubgroup{kind: r.Type, label: typeLabel(r.Type)})
			sg = &g.subgroups[len(g.subgroups)-1]
		}
		sg.items = append(sg.items, r)
	}
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		aPkg, bPkg := strings.HasPrefix(a.key, "package:"), strings.HasPrefix(b.key, "package:")
		if aPkg != bPkg {
			return aPkg
		}
		aUser, bUser := strings.Contains(a.key, ":user:"), strings.Contains(b.key, ":user:")
		if aUser != bUser {
			return aUser
		}
		return a.label < b.label
	})
	typeOrder := map[string]int{
		pkgmgr.KindExtensions: 0, pkgmgr.KindSkills: 1, pkgmgr.KindPrompts: 2, pkgmgr.KindThemes: 3,
	}
	for i := range groups {
		sort.Slice(groups[i].subgroups, func(a, b int) bool {
			return typeOrder[groups[i].subgroups[a].kind] < typeOrder[groups[i].subgroups[b].kind]
		})
		for j := range groups[i].subgroups {
			items := groups[i].subgroups[j].items
			sort.Slice(items, func(a, b int) bool {
				return displayName(items[a]) < displayName(items[b])
			})
			groups[i].subgroups[j].items = items
		}
	}
	return groups
}

func flattenGroups(groups []configGroup) []configRow {
	var rows []configRow
	for _, g := range groups {
		rows = append(rows, configRow{kind: configRowGroup, label: g.label})
		for _, sg := range g.subgroups {
			rows = append(rows, configRow{kind: configRowSubgroup, label: sg.label})
			for _, item := range sg.items {
				rows = append(rows, configRow{kind: configRowItem, label: displayName(item), resource: item})
			}
		}
	}
	return rows
}

// NewConfigSelector loads discovered resources for the config TUI.
func NewConfigSelector(mgr *pkgmgr.Manager, writeProject bool) (*ConfigSelector, error) {
	m := &ConfigSelector{
		mgr:          mgr,
		writeProject: writeProject && mgr.Trusted,
		projectOK:    mgr.Trusted,
		width:        80,
		height:       24,
		titleStyle:   lipgloss.NewStyle().Bold(true),
		hintStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		groupStyle:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")),
		subStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		onStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		offStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		plusStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		minusStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("178")),
		dimStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		cursorStyle:  lipgloss.NewStyle().Reverse(true),
		mutedStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
	}
	if err := m.reload(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *ConfigSelector) reload() error {
	ctx := context.Background()
	global, err := m.mgr.ResolveGlobal(ctx)
	if err != nil {
		return err
	}
	m.global = global
	m.inherited = map[string]bool{}
	for _, r := range global {
		m.inherited[resourceKey(r)] = r.Enabled
	}
	if m.projectOK {
		project, err := m.mgr.Resolve(ctx)
		if err != nil {
			return err
		}
		m.project = project
	} else {
		m.project = global
	}
	m.rebuildRows()
	return nil
}

func (m *ConfigSelector) currentResources() []pkgmgr.Resource {
	if m.writeProject {
		return m.project
	}
	return m.global
}

func (m *ConfigSelector) rebuildRows() {
	prev := ""
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == configRowItem {
		prev = resourceKey(m.rows[m.cursor].resource)
	}
	m.rows = flattenGroups(buildConfigGroups(m.currentResources(), m.mgr.AgentDir))
	m.cursor = 0
	if prev != "" {
		for i, row := range m.rows {
			if row.kind == configRowItem && resourceKey(row.resource) == prev {
				m.cursor = i
				break
			}
		}
	}
	m.cursor = m.nearestItem(m.cursor, 1)
}

func (m *ConfigSelector) nearestItem(start, dir int) int {
	if len(m.rows) == 0 {
		return 0
	}
	i := start
	if i < 0 {
		i = 0
	}
	if i >= len(m.rows) {
		i = len(m.rows) - 1
	}
	for i >= 0 && i < len(m.rows) {
		if m.rows[i].kind == configRowItem {
			return i
		}
		i += dir
	}
	for j, row := range m.rows {
		if row.kind == configRowItem {
			return j
		}
	}
	return 0
}

func (m *ConfigSelector) currentItem() (pkgmgr.Resource, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return pkgmgr.Resource{}, false
	}
	row := m.rows[m.cursor]
	if row.kind != configRowItem {
		return pkgmgr.Resource{}, false
	}
	return row.resource, true
}

func (m *ConfigSelector) inheritedEnabled(r pkgmgr.Resource) bool {
	if v, ok := m.inherited[resourceKey(r)]; ok {
		return v
	}
	if r.Scope == "user" {
		return r.Enabled
	}
	return true
}

func (m *ConfigSelector) checkbox(r pkgmgr.Resource) string {
	if m.writeProject {
		switch m.mgr.ProjectOverrideState(r) {
		case pkgmgr.OverrideLoad:
			return m.plusStyle.Render("[+]")
		case pkgmgr.OverrideUnload:
			return m.minusStyle.Render("[-]")
		default:
			if r.Enabled {
				return m.dimStyle.Render("[x]")
			}
			return m.dimStyle.Render("[ ]")
		}
	}
	if r.Enabled {
		return m.onStyle.Render("[x]")
	}
	return m.offStyle.Render("[ ]")
}

func (m *ConfigSelector) suffix(r pkgmgr.Resource) string {
	if !m.writeProject {
		return ""
	}
	switch m.mgr.ProjectOverrideState(r) {
	case pkgmgr.OverrideLoad:
		return m.mutedStyle.Render("  project load")
	case pkgmgr.OverrideUnload:
		return m.mutedStyle.Render("  project unload")
	default:
		if r.Scope == "user" {
			return m.dimStyle.Render("  inherited global")
		}
	}
	return ""
}

func (m *ConfigSelector) dimmed(r pkgmgr.Resource) bool {
	return m.writeProject && r.Scope == "user" && m.mgr.ProjectOverrideState(r) == pkgmgr.OverrideInherit
}

// Init implements tea.Model.
func (m *ConfigSelector) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *ConfigSelector) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.quit = true
			return m, tea.Quit
		case "up", "k":
			m.cursor = m.nearestItem(m.cursor-1, -1)
		case "down", "j":
			m.cursor = m.nearestItem(m.cursor+1, 1)
		case "pgup":
			m.cursor = m.nearestItem(m.cursor-m.visibleRows(), -1)
		case "pgdown":
			m.cursor = m.nearestItem(m.cursor+m.visibleRows(), 1)
		case "tab":
			if m.projectOK {
				m.writeProject = !m.writeProject
				m.rebuildRows()
			}
		case " ", "space", "enter":
			m.toggle()
		}
	}
	return m, nil
}

func (m *ConfigSelector) visibleRows() int {
	n := m.height - 4
	if n < 3 {
		return 3
	}
	return n
}

func (m *ConfigSelector) toggle() {
	r, ok := m.currentItem()
	if !ok {
		return
	}
	if !m.writeProject && r.Scope != "user" {
		return
	}
	if err := m.mgr.ToggleResource(r, m.writeProject, m.inheritedEnabled(r)); err != nil {
		m.err = err
		return
	}
	_ = m.reload()
}

// View implements tea.Model.
func (m *ConfigSelector) View() string {
	if m.quit {
		return ""
	}
	w := m.width
	if w <= 0 {
		w = 80
	}
	title := "Global Resources"
	if m.writeProject {
		title = "Project Local Resources"
	}
	hints := "space toggle · esc close"
	if m.writeProject {
		hints = "space cycle inherit/+/- · esc close"
	}
	if m.projectOK {
		hints = "tab switch mode · " + hints
	}
	header := m.titleStyle.Render(title) + strings.Repeat(" ", maxInt(1, w-lipgloss.Width(title)-lipgloss.Width(hints))) + m.hintStyle.Render(hints)
	pathHint := m.hintStyle.Render("~/.pigo/agent/settings.json")
	if m.writeProject {
		pathHint = m.hintStyle.Render(".pigo/settings.json · inherited global resources are dimmed")
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString(pathHint)
	b.WriteByte('\n')
	if m.err != nil {
		b.WriteString(m.minusStyle.Render(m.err.Error()))
		b.WriteByte('\n')
	}
	if len(m.rows) == 0 {
		b.WriteString(m.mutedStyle.Render("No resources found."))
		b.WriteByte('\n')
		return b.String()
	}
	visible := m.visibleRows()
	start := 0
	if m.cursor >= visible {
		start = m.cursor - visible + 1
	}
	end := start + visible
	if end > len(m.rows) {
		end = len(m.rows)
	}
	for i := start; i < end; i++ {
		row := m.rows[i]
		line := ""
		switch row.kind {
		case configRowGroup:
			line = m.groupStyle.Render(row.label)
		case configRowSubgroup:
			line = "  " + m.subStyle.Render(row.label)
		default:
			mark := "  "
			if i == m.cursor {
				mark = "> "
			}
			body := mark + m.checkbox(row.resource) + "  " + row.label + m.suffix(row.resource)
			if m.dimmed(row.resource) {
				body = m.dimStyle.Render(mark + "[x]  " + row.label + m.suffix(row.resource))
				if !row.resource.Enabled {
					body = m.dimStyle.Render(mark + "[ ]  " + row.label + m.suffix(row.resource))
				}
			} else if i == m.cursor {
				body = m.cursorStyle.Render(body)
			}
			line = body
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RunConfigSelector starts the resource enable/disable TUI.
func RunConfigSelector(mgr *pkgmgr.Manager, writeProject bool) error {
	m, err := NewConfigSelector(mgr, writeProject)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
