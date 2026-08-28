package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/models"
)

type scopedRefreshMsg struct {
	gen      int
	failed   []string
	timedOut bool
}

var scopedCatalogRefresh = defaultScopedCatalogRefresh

func defaultScopedCatalogRefresh(ctx context.Context, agentDir, baseURL string) []string {
	if agentDir == "" || baseURL == "" {
		return nil
	}
	return models.RefreshAll(ctx, models.OpenFileStore(filepath.Join(agentDir, "models-store.json")), baseURL, true)
}

type scopedModelsPicker struct {
	listPicker
	enabled           enabledIDs
	available         []models.Model
	allIDs            []string
	dirty             bool
	refreshStatus     string
	openedEmptyScoped bool
	gen               int
	cancel            context.CancelFunc
}

func (m Model) scopedModelsActive() bool { return m.scoped.active }

func (m Model) openScopedModels() (tea.Model, tea.Cmd) {
	avail := m.catalogModels()
	allIDs := availableIDList(avail)
	patterns := m.cfg.EnabledModels
	if m.engine != nil && m.engine.Opts.UserConfig != nil {
		patterns = m.engine.Opts.UserConfig.EnabledModels
	}
	openedEmpty := m.engine == nil || len(m.engine.Scoped) == 0
	var en enabledIDs
	if m.engine != nil && len(m.engine.Scoped) > 0 {
		ids := make([]string, 0, len(m.engine.Scoped))
		for _, s := range m.engine.Scoped {
			ids = append(ids, s.Provider+"/"+s.ID)
		}
		en = enabledIDs{ids: ids}
	} else if len(patterns) == 0 {
		en = enabledIDs{all: true}
	} else {
		resolved := models.ResolvePatternsIn(patterns, avail)
		ids := make([]string, 0, len(resolved))
		for _, s := range resolved {
			ids = append(ids, s.Provider+"/"+s.ID)
		}
		ids = append(ids, models.UnmatchedPatterns(patterns, avail)...)
		en = enabledIDs{ids: ids}
	}
	saveHint := "ctrl+s"
	if m.keys != nil {
		if ks := m.keys.Keys("app.models.save"); len(ks) > 0 {
			saveHint = ks[0]
		}
	}
	m.scopedGen++
	p := scopedModelsPicker{
		listPicker: listPicker{
			title:  "Model Configuration\nSession-only. " + saveHint + " to save to settings.",
			active: true,
		},
		enabled:           en,
		available:         avail,
		allIDs:            allIDs,
		openedEmptyScoped: openedEmpty,
		gen:               m.scopedGen,
	}
	p.rebuild()
	m.scoped = p
	return m, m.startScopedRefresh()
}

func (m *Model) startScopedRefresh() tea.Cmd {
	if m.engine == nil || m.engine.Opts.Offline || m.engine.Opts.CatalogBaseURL == "" {
		return nil
	}
	if os.Getenv("PIGO_OFFLINE") != "" {
		return nil
	}
	gen := m.scoped.gen
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	m.scoped.cancel = cancel
	m.scoped.refreshStatus = "Refreshing model catalogs…"
	m.scoped.rebuild()
	dir := m.engine.Opts.AgentDir
	base := m.engine.Opts.CatalogBaseURL
	return func() tea.Msg {
		failed := scopedCatalogRefresh(ctx, dir, base)
		timedOut := ctx.Err() == context.DeadlineExceeded
		return scopedRefreshMsg{gen: gen, failed: failed, timedOut: timedOut}
	}
}

func (m Model) handleScopedRefresh(msg scopedRefreshMsg) (tea.Model, tea.Cmd) {
	if !m.scoped.active || msg.gen != m.scoped.gen {
		return m, nil
	}
	if m.scoped.cancel != nil {
		m.scoped.cancel()
		m.scoped.cancel = nil
	}
	m.scoped.available = m.catalogModels()
	m.scoped.allIDs = availableIDList(m.scoped.available)
	if !m.scoped.dirty && m.scoped.openedEmptyScoped {
		patterns := m.cfg.EnabledModels
		if m.engine != nil && m.engine.Opts.UserConfig != nil {
			patterns = m.engine.Opts.UserConfig.EnabledModels
		}
		if len(patterns) == 0 {
			m.scoped.enabled = enabledIDs{all: true}
		} else {
			resolved := models.ResolvePatternsIn(patterns, m.scoped.available)
			ids := make([]string, 0, len(resolved))
			for _, s := range resolved {
				ids = append(ids, s.Provider+"/"+s.ID)
			}
			ids = append(ids, models.UnmatchedPatterns(patterns, m.scoped.available)...)
			m.scoped.enabled = enabledIDs{ids: ids}
		}
	}
	if !m.scoped.enabled.all {
		m.applyScopedSession()
	}
	switch {
	case msg.timedOut:
		m.scoped.refreshStatus = "Model refresh timed out; showing cached models."
	case len(msg.failed) > 0:
		m.scoped.refreshStatus = "Could not refresh " + strings.Join(msg.failed, ", ") + "; showing cached models."
	default:
		m.scoped.refreshStatus = "Model catalogs refreshed."
	}
	m.scoped.rebuild()
	return m, nil
}

func availableIDList(available []models.Model) []string {
	ids := make([]string, 0, len(available))
	for _, mod := range available {
		ids = append(ids, mod.Provider+"/"+mod.ID)
	}
	return ids
}

func (p *scopedModelsPicker) rebuild() {
	keep := ""
	if current, ok := p.current(); ok {
		keep = current.ID
	}
	p.rebuildKeeping(keep)
}

func (p *scopedModelsPicker) rebuildKeeping(keep string) {
	available := make(map[string]models.Model, len(p.available))
	for _, mod := range p.available {
		available[mod.Provider+"/"+mod.ID] = mod
	}
	ids := sortedIDs(p.enabled, p.allIDs)
	items := make([]pickerItem, 0, len(ids))
	for _, id := range ids {
		mod, ok := available[id]
		label := id
		meta := "[unavailable]"
		aux := ""
		if ok {
			label = mod.ID
			meta = "[" + mod.Provider + "]"
			aux = mod.ID
		}
		if !p.enabled.all {
			if p.enabled.isEnabled(id) {
				label += " ✓"
			} else {
				label += " ✗"
			}
		}
		items = append(items, pickerItem{ID: id, Label: label, Meta: meta, Aux: aux})
	}
	p.setItems(items)
	if keep != "" {
		for i, item := range p.filtered {
			if item.ID == keep {
				p.selected = i
				break
			}
		}
	}
	p.hint = "enter toggle · ctrl+a all · ctrl+x clear · ctrl+p provider · alt+up/alt+down reorder · ctrl+s save · " + p.countText(available)
	if p.dirty {
		p.hint += " (unsaved)"
	}
}

func (p scopedModelsPicker) countText(available map[string]models.Model) string {
	if p.enabled.all {
		return "all enabled"
	}
	enabled := 0
	unavailable := 0
	for _, id := range p.enabled.ids {
		if _, ok := available[id]; ok {
			enabled++
		} else {
			unavailable++
		}
	}
	count := strconv.Itoa(enabled) + "/" + strconv.Itoa(len(p.available)) + " enabled"
	if unavailable > 0 {
		count += " · " + strconv.Itoa(unavailable) + " unavailable"
	}
	return count
}

func (p scopedModelsPicker) view() string {
	var b strings.Builder
	b.WriteString(p.title)
	b.WriteString("\n")
	b.WriteString("> ")
	b.WriteString(p.query)
	b.WriteString("\n\n")
	n := len(p.filtered)
	if n == 0 {
		b.WriteString("  (no matches)\n")
		if p.refreshStatus != "" {
			b.WriteString("\n")
			b.WriteString(p.refreshStatus)
		}
		if p.hint != "" {
			b.WriteString("\n")
			b.WriteString(p.hint)
		}
		return b.String()
	}
	start := max(0, min(p.selected-pickerMaxVisible/2, n-pickerMaxVisible))
	end := min(start+pickerMaxVisible, n)
	for i := start; i < end; i++ {
		item := p.filtered[i]
		mark := "  "
		if i == p.selected {
			mark = "→ "
		}
		b.WriteString(mark)
		b.WriteString(item.Label)
		if item.Meta != "" {
			b.WriteString(" ")
			b.WriteString(item.Meta)
		}
		b.WriteByte('\n')
	}
	if start > 0 || end < n {
		b.WriteString("  (")
		b.WriteString(strconv.Itoa(p.selected + 1))
		b.WriteByte('/')
		b.WriteString(strconv.Itoa(n))
		b.WriteString(")\n")
	}
	if item, ok := p.current(); ok {
		b.WriteString("\n  ")
		if item.Aux == "" {
			b.WriteString("Model unavailable")
		} else {
			b.WriteString("Model Name: ")
			b.WriteString(item.Aux)
		}
		b.WriteByte('\n')
	}
	if p.refreshStatus != "" {
		b.WriteString("\n")
		b.WriteString(p.refreshStatus)
	}
	if p.hint != "" {
		b.WriteString("\n")
		b.WriteString(p.hint)
	}
	return b.String()
}

func (m Model) handleScopedModelsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.scoped
	if m.keyIs(msg, "app.models.save") {
		m.persistScopedModels()
		return m, nil
	}
	if m.keyIs(msg, "app.models.enableAll") {
		p.enabled = enableAllIDs(p.enabled, p.allIDs, p.filteredTargets())
		p.dirty = true
		p.rebuild()
		m.applyScopedSession()
		return m, nil
	}
	if m.keyIs(msg, "app.models.clearAll") {
		p.enabled = clearAllIDs(p.enabled, p.allIDs, p.filteredTargets())
		p.dirty = true
		p.rebuild()
		m.applyScopedSession()
		return m, nil
	}
	if m.keyIs(msg, "app.models.toggleProvider") {
		if item, ok := p.current(); ok {
			if mod, found := p.availableModel(item.ID); found {
				p.toggleProvider(mod.Provider)
				p.dirty = true
				p.rebuild()
				m.applyScopedSession()
			}
		}
		return m, nil
	}
	if m.keyIs(msg, "app.models.reorderUp") {
		m.reorderScopedModel(-1)
		return m, nil
	}
	if m.keyIs(msg, "app.models.reorderDown") {
		m.reorderScopedModel(1)
		return m, nil
	}
	if m.keyIs(msg, "app.clear") {
		if p.query != "" {
			p.query = ""
			p.rebuild()
		} else {
			m.closeScopedModels()
		}
		return m, nil
	}

	switch p.handleKey(msg, m.keys) {
	case "confirm":
		if item, ok := p.current(); ok {
			p.enabled = toggleID(p.enabled, item.ID)
			p.dirty = true
			p.rebuild()
			m.applyScopedSession()
		}
	case "cancel":
		m.closeScopedModels()
	}
	return m, nil
}

func (p scopedModelsPicker) filteredTargets() []string {
	if p.query == "" {
		return nil
	}
	targets := make([]string, 0, len(p.filtered))
	for _, item := range p.filtered {
		targets = append(targets, item.ID)
	}
	return targets
}

func (p scopedModelsPicker) availableModel(id string) (models.Model, bool) {
	for _, mod := range p.available {
		if mod.Provider+"/"+mod.ID == id {
			return mod, true
		}
	}
	return models.Model{}, false
}

func (p *scopedModelsPicker) toggleProvider(provider string) {
	prefix := provider + "/"
	var targets []string
	allEnabled := true
	for _, id := range p.allIDs {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		targets = append(targets, id)
		if !p.enabled.isEnabled(id) {
			allEnabled = false
		}
	}
	if allEnabled {
		p.enabled = clearAllIDs(p.enabled, p.allIDs, targets)
		return
	}
	p.enabled = enableAllIDs(p.enabled, p.allIDs, targets)
}

func (m *Model) reorderScopedModel(delta int) {
	p := &m.scoped
	item, ok := p.current()
	if !ok || p.enabled.all || !p.enabled.isEnabled(item.ID) {
		return
	}
	index := -1
	for i, id := range p.enabled.ids {
		if id == item.ID {
			index = i
			break
		}
	}
	if index < 0 || index+delta < 0 || index+delta >= len(p.enabled.ids) {
		return
	}
	p.enabled = moveID(p.enabled, item.ID, delta)
	p.selected += delta
	p.dirty = true
	p.rebuildKeeping(item.ID)
	m.applyScopedSession()
}

func (m *Model) applyScopedSession() {
	p := &m.scoped
	ids, implicit := sessionScopeIDs(p.enabled, availableIDList(p.available))
	if implicit || m.engine == nil {
		if m.engine != nil {
			m.engine.SetScopedModels(nil)
		}
		return
	}
	m.engine.SetScopedModels(models.ResolvePatternsIn(ids, p.available))
}

func (m *Model) persistScopedModels() {
	p := &m.scoped
	available := make(map[string]bool, len(p.available))
	for _, id := range p.allIDs {
		available[id] = true
	}
	implicit := p.enabled.all
	if !implicit && len(p.enabled.ids) == len(p.available) {
		implicit = true
		for _, id := range p.enabled.ids {
			if !available[id] {
				implicit = false
				break
			}
		}
	}

	var patterns *[]string
	if !implicit {
		ids := append([]string(nil), p.enabled.ids...)
		patterns = &ids
	}
	if m.engine != nil {
		if err := m.engine.PersistEnabledModels(patterns); err != nil {
			m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("Could not save model selection: " + err.Error())})
			return
		}
	}
	if patterns == nil {
		m.cfg.EnabledModels = nil
	} else {
		m.cfg.EnabledModels = append([]string(nil), (*patterns)...)
	}
	p.dirty = false
	p.rebuild()
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("Model selection saved to settings")})
}

func (m *Model) closeScopedModels() {
	if m.scoped.cancel != nil {
		m.scoped.cancel()
	}
	m.scoped = scopedModelsPicker{}
}
