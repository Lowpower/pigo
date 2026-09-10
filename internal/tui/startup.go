package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Lowpower/pigo/internal/keys"
	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/skills"
	"github.com/Lowpower/pigo/internal/version"
)

func (m Model) showStartupListing() bool {
	return !m.cfg.QuietStartup()
}

func (m Model) startupView() string {
	if !m.showStartupListing() {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.startupHeader())
	b.WriteString("\n\n")
	if line := m.scopedModelsLine(); line != "" {
		b.WriteString(m.metaStyle.Render(line))
		b.WriteString("\n\n")
	}
	if res := m.startupResources(); res != "" {
		b.WriteString(res)
	}
	return b.String()
}

func (m Model) startupHeader() string {
	kb := m.startupKeys()
	logo := m.titleStyle.Render("pigo") + m.metaStyle.Render(" v"+version.Version)
	onboarding := m.metaStyle.Render("pigo can explain its own features and look up its docs.") + "\n" + m.metaStyle.Render("Ask it how to use or extend pigo.")
	if m.toolsExpanded {
		return logo + "\n" + m.expandedStartupHints(kb) + "\n\n" + onboarding
	}
	compact := strings.Join([]string{
		startupKeyHint(kb, "app.interrupt", "interrupt"),
		startupRawHint(startupKeyText(kb, "app.clear")+"/"+startupKeyText(kb, "app.exit"), "clear/exit"),
		startupRawHint("/", "commands"),
		startupRawHint("!", "bash"),
		startupKeyHint(kb, "app.tools.expand", "more"),
	}, " · ")
	expandKey := startupKeyText(kb, "app.tools.expand")
	if expandKey == "" {
		expandKey = "ctrl+o"
	}
	compactOnboarding := m.metaStyle.Render("Press " + expandKey + " to show full startup help and loaded resources.")
	return logo + "\n" + m.metaStyle.Render(compact) + "\n" + compactOnboarding + "\n\n" + onboarding
}

func (m Model) expandedStartupHints(kb *keys.Manager) string {
	lines := []string{
		startupKeyHint(kb, "app.interrupt", "to interrupt"),
		startupKeyHint(kb, "app.clear", "to clear"),
		startupRawHint(startupKeyText(kb, "app.clear")+" twice", "to exit"),
		startupKeyHint(kb, "app.exit", "to exit (empty)"),
		startupKeyHint(kb, "app.suspend", "to suspend"),
		startupKeyHint(kb, "tui.editor.deleteToLineEnd", "to delete to end"),
		startupKeyHint(kb, "app.thinking.cycle", "to cycle thinking level"),
		startupRawHint(startupKeyText(kb, "app.model.cycleForward")+"/"+startupKeyText(kb, "app.model.cycleBackward"), "to cycle models"),
		startupKeyHint(kb, "app.model.select", "to select model"),
		startupKeyHint(kb, "app.tools.expand", "to expand tools"),
		startupKeyHint(kb, "app.thinking.toggle", "to expand thinking"),
		startupKeyHint(kb, "app.editor.external", "for external editor"),
		startupRawHint("/", "for commands"),
		startupRawHint("!", "to run bash"),
		startupRawHint("!!", "to run bash (no context)"),
		startupKeyHint(kb, "app.message.followUp", "to queue follow-up"),
		startupKeyHint(kb, "app.message.dequeue", "to edit all queued messages"),
		startupKeyHint(kb, "app.clipboard.pasteImage", "to paste image"),
		startupRawHint("drop files", "to attach"),
	}
	return m.metaStyle.Render(strings.Join(lines, "\n"))
}

func (m Model) scopedModelsLine() string {
	if m.engine == nil || len(m.engine.Scoped) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.engine.Scoped))
	for _, sm := range m.engine.Scoped {
		id := sm.ID
		if sm.Provider != "" && id != "" {
			id = sm.Provider + "/" + sm.ID
		}
		if sm.Thinking != "" {
			id += ":" + sm.Thinking
		}
		if id != "" {
			parts = append(parts, id)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	line := "Model scope: " + strings.Join(parts, ", ")
	if cycle := startupKeyText(m.startupKeys(), "app.model.cycleForward"); cycle != "" {
		line += " (" + cycle + " to cycle)"
	}
	return line
}

func (m Model) startupResources() string {
	home, _ := os.UserHomeDir()
	cwd := ""
	agentDir := ""
	trusted := false
	if m.engine != nil {
		cwd = m.engine.Opts.Cwd
		agentDir = m.engine.Opts.AgentDir
		trusted = m.engine.Opts.ProjectTrusted
	}
	var sections []string
	if m.engine == nil || !m.engine.Opts.NoContextFiles {
		if files := prompt.ContextFilePaths(cwd, agentDir, trusted); len(files) > 0 {
			compact := make([]string, 0, len(files))
			expanded := make([]string, 0, len(files))
			for _, p := range files {
				compact = append(compact, formatCompactContextPath(p, cwd, home))
				expanded = append(expanded, formatDisplayPath(p, home))
			}
			sections = append(sections, m.loadedSection("Context", joinCompact(compact, false), joinExpanded(expanded)))
		}
	}
	if m.engine != nil {
		if names, paths := skillListing(m.engine.Skills); len(names) > 0 {
			disp := make([]string, len(paths))
			for i, p := range paths {
				disp[i] = formatDisplayPath(p, home)
			}
			sections = append(sections, m.loadedSection("Skills", joinCompact(names, true), joinExpanded(disp)))
		}
		if names, paths := templateListing(m.engine.Templates); len(names) > 0 {
			disp := make([]string, len(paths))
			for i, p := range paths {
				disp[i] = formatDisplayPath(p, home)
			}
			sections = append(sections, m.loadedSection("Prompts", joinCompact(names, true), joinExpanded(disp)))
		}
		if names, paths := extensionListing(m); len(names) > 0 {
			disp := make([]string, len(paths))
			for i, p := range paths {
				if p == "" {
					disp[i] = names[i]
					continue
				}
				disp[i] = formatDisplayPath(p, home)
			}
			sections = append(sections, m.loadedSection("Extensions", joinCompact(names, true), joinExpanded(disp)))
		}
		if names, paths := themeListing(m); len(names) > 0 {
			disp := make([]string, len(paths))
			for i, p := range paths {
				disp[i] = formatDisplayPath(p, home)
			}
			sections = append(sections, m.loadedSection("Themes", joinCompact(names, true), joinExpanded(disp)))
		}
	}
	return strings.Join(sections, "\n\n")
}

func (m Model) loadedSection(name, compact, expanded string) string {
	title := m.titleStyle.Render("[" + name + "]")
	body := compact
	if m.toolsExpanded {
		body = expanded
	}
	return title + "\n" + m.metaStyle.Render(body)
}

func skillListing(list []skills.Skill) (names, paths []string) {
	for _, s := range list {
		if s.Name == "" {
			continue
		}
		names = append(names, s.Name)
		paths = append(paths, s.FilePath)
	}
	return names, paths
}

func templateListing(list []prompt.Template) (names, paths []string) {
	for _, t := range list {
		if t.Name == "" {
			continue
		}
		names = append(names, t.Name)
		paths = append(paths, t.FilePath)
	}
	return names, paths
}

func extensionListing(m Model) (names, paths []string) {
	seen := map[string]bool{}
	add := func(name, path string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
		paths = append(paths, path)
	}
	for _, h := range m.engine.Hosts {
		if h == nil {
			continue
		}
		add(h.Name(), "")
	}
	for _, spec := range m.engine.Opts.Extensions {
		add(spec, spec)
	}
	return names, paths
}

func themeListing(m Model) (names, paths []string) {
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		key := p
		if abs, err := filepath.Abs(p); err == nil {
			key = abs
		}
		if seen[key] {
			return
		}
		seen[key] = true
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		if base == "" {
			base = p
		}
		names = append(names, base)
		paths = append(paths, p)
	}
	for _, p := range m.engine.Opts.ThemePaths {
		add(p)
	}
	if m.engine.Opts.NoThemes {
		return names, paths
	}
	for _, p := range m.engine.ThemeFiles {
		add(p)
	}
	return names, paths
}

func (m Model) startupKeys() *keys.Manager {
	if m.keys != nil {
		return m.keys
	}
	return keys.NewManager("")
}

func startupKeyText(kb *keys.Manager, action string) string {
	if kb == nil {
		return ""
	}
	for _, k := range kb.Keys(action) {
		if label := formatStartupKey(k); label != "" {
			return label
		}
	}
	return ""
}

func startupKeyHint(kb *keys.Manager, action, desc string) string {
	if label := startupKeyText(kb, action); label != "" {
		return label + " " + desc
	}
	return desc
}

func startupRawHint(key, desc string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return desc
	}
	return key + " " + desc
}

func formatStartupKey(k string) string {
	k = keys.Normalize(k)
	if k == "" {
		return ""
	}
	if k == "esc" {
		return "escape"
	}
	return k
}

func formatDisplayPath(path, home string) string {
	if path == "" {
		return ""
	}
	if home != "" {
		if path == home {
			return "~"
		}
		sep := string(os.PathSeparator)
		if strings.HasPrefix(path, home+sep) {
			return "~" + path[len(home):]
		}
	}
	return path
}

func formatCompactContextPath(path, cwd, home string) string {
	if cwd != "" {
		if path == cwd {
			return "."
		}
		rel, err := filepath.Rel(cwd, path)
		if err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return formatDisplayPath(path, home)
}

func joinCompact(items []string, sortNames bool) string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			labels = append(labels, item)
		}
	}
	if sortNames {
		sort.Strings(labels)
	}
	if len(labels) == 0 {
		return ""
	}
	return "  " + strings.Join(labels, ", ")
}

func joinExpanded(items []string) string {
	var lines []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			lines = append(lines, "  "+item)
		}
	}
	return strings.Join(lines, "\n")
}
