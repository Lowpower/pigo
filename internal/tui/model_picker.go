package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
)

type modelScope string

const (
	scopeAll    modelScope = "all"
	scopeScoped modelScope = "scoped"
)

type modelPicker struct {
	listPicker
	scope       modelScope
	all         []pickerItem
	scoped      []pickerItem
	currentProv string
	currentID   string
	defaultProv string
	defaultID   string
}

func (m Model) handleModelCommand(rest string) (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(s)})
		return m, nil
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return m.openModelPicker("")
	}
	if mod, ok := exactModel(rest, m.catalogModels()); ok {
		_, _, thinking := models.ParseSpec(rest)
		m.applySessionModel(mod.Provider, mod.ID, thinking, false)
		return note("model = " + m.cfg.ResolvedProvider() + "/" + m.cfg.ResolvedModel())
	}
	return m.openModelPicker(rest)
}

func exactModel(search string, list []models.Model) (models.Model, bool) {
	s := strings.ToLower(strings.TrimSpace(search))
	_, _, thinking := models.ParseSpec(search)
	if thinking != "" {
		if i := strings.LastIndex(search, ":"); i >= 0 {
			s = strings.ToLower(strings.TrimSpace(search[:i]))
		}
	}
	var canon []models.Model
	for _, mod := range list {
		if strings.ToLower(mod.Provider+"/"+mod.ID) == s {
			canon = append(canon, mod)
		}
	}
	if len(canon) == 1 {
		return canon[0], true
	}
	var ids []models.Model
	for _, mod := range list {
		if strings.ToLower(mod.ID) == s {
			ids = append(ids, mod)
		}
	}
	if len(ids) == 1 {
		return ids[0], true
	}
	return models.Model{}, false
}

func (m Model) openModelPicker(search string) (tea.Model, tea.Cmd) {
	p := modelPicker{
		currentProv: m.cfg.ResolvedProvider(),
		currentID:   m.cfg.ResolvedModel(),
		defaultProv: m.cfg.DefaultProvider,
		defaultID:   m.cfg.DefaultModel,
	}
	if p.defaultProv == "" {
		p.defaultProv = p.currentProv
	}
	if p.defaultID == "" {
		p.defaultID = p.currentID
	}
	p.all = modelItems(m.catalogModels(), p)
	if m.engine != nil {
		for _, s := range m.engine.Scoped {
			p.scoped = append(p.scoped, p.formatItem(s.Model))
		}
	}
	p.scope = scopeAll
	if len(p.scoped) > 0 {
		p.scope = scopeScoped
	}
	p.query = search
	p.active = true
	p.refresh()
	m.models = p
	return m, nil
}

func (m Model) catalogModels() []models.Model {
	if m.engine != nil && m.engine.Opts.AgentDir != "" {
		ids := auth.AuthenticatedIDs(auth.Open(m.engine.Opts.AgentDir))
		if list := models.Available(ids); len(list) > 0 {
			return list
		}
	}
	return models.Catalog()
}

func modelItems(list []models.Model, p modelPicker) []pickerItem {
	items := make([]pickerItem, 0, len(list))
	for _, mod := range list {
		items = append(items, p.formatItem(mod))
	}
	return sortModelItems(items, p)
}

func (p modelPicker) formatItem(mod models.Model) pickerItem {
	label := mod.ID
	if p.currentProv == mod.Provider && p.currentID == mod.ID {
		label += " ✓"
	}
	if p.defaultProv == mod.Provider && p.defaultID == mod.ID {
		label += " · default"
	}
	return pickerItem{
		ID:    mod.Provider + "/" + mod.ID,
		Label: label,
		Meta:  "[" + mod.Provider + "]",
		Aux:   mod.ID,
	}
}

func sortModelItems(items []pickerItem, p modelPicker) []pickerItem {
	cur := p.currentProv + "/" + p.currentID
	def := p.defaultProv + "/" + p.defaultID
	out := append([]pickerItem(nil), items...)
	less := func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ID == cur && b.ID != cur {
			return true
		}
		if a.ID != cur && b.ID == cur {
			return false
		}
		if a.ID == def && b.ID != def {
			return true
		}
		if a.ID != def && b.ID == def {
			return false
		}
		ap := strings.SplitN(a.ID, "/", 2)[0]
		bp := strings.SplitN(b.ID, "/", 2)[0]
		if ap != bp {
			return ap < bp
		}
		return a.ID < b.ID
	}
	// insertion sort keeps this tiny and avoids extra imports
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && less(j, j-1) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

func (p *modelPicker) refresh() {
	if p.scope == scopeScoped && len(p.scoped) > 0 {
		p.setItems(p.scoped)
	} else {
		p.setItems(p.all)
	}
	p.title = p.scopeTitle()
	p.hint = p.scopeHint()
}

func (p modelPicker) scopeTitle() string {
	if len(p.scoped) == 0 {
		return "Select model\nOnly showing models from configured providers. Use /login to add providers."
	}
	all := "all"
	scoped := "scoped"
	if p.scope == scopeAll {
		all = "[" + all + "]"
	} else {
		scoped = "[" + scoped + "]"
	}
	return "Select model\nScope: " + all + " | " + scoped
}

func (p modelPicker) scopeHint() string {
	if len(p.scoped) > 0 {
		return "↑↓ select · Tab scope · Enter select · Ctrl+S default · Esc cancel"
	}
	return "↑↓ select · Enter select · Ctrl+S default · Esc cancel"
}

func (p *modelPicker) toggleScope() {
	if len(p.scoped) == 0 {
		return
	}
	if p.scope == scopeAll {
		p.scope = scopeScoped
	} else {
		p.scope = scopeAll
	}
	p.refresh()
}

func (m Model) handleModelPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	act := m.models.handleKey(msg, m.keys)
	switch act {
	case "tab":
		m.models.toggleScope()
		return m, nil
	case "cancel":
		m.models = modelPicker{}
		return m, nil
	case "confirm":
		return m.applyPickerModel(false)
	case "save":
		return m.applyPickerModel(true)
	}
	return m, nil
}

func (m Model) applyPickerModel(persist bool) (tea.Model, tea.Cmd) {
	it, ok := m.models.current()
	if !ok {
		return m, nil
	}
	prov, id, _ := strings.Cut(it.ID, "/")
	m.applySessionModel(prov, id, "", persist)
	m.models = modelPicker{}
	msg := "model = " + m.cfg.ResolvedProvider() + "/" + m.cfg.ResolvedModel()
	if persist {
		msg = "Default model: " + m.cfg.DefaultProvider + "/" + m.cfg.DefaultModel
	}
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(msg)})
	return m, nil
}

func (m *Model) applySessionModel(prov, id, thinking string, persist bool) {
	if m.engine != nil {
		if persist {
			_ = m.engine.PersistModel(prov, id, thinking)
		} else {
			m.engine.ApplyModel(prov, id, thinking)
		}
		m.cfg = m.engine.Opts.Config
		m.provider = m.engine.Provider
		return
	}
	if prov != "" {
		m.cfg.Provider = prov
		if persist {
			m.cfg.DefaultProvider = prov
		}
	}
	if id != "" {
		m.cfg.Model = id
		if persist {
			m.cfg.DefaultModel = id
		}
	}
	if thinking != "" {
		m.cfg.Thinking = thinking
	}
	if persist {
		_ = config.Save(config.DefaultConfigDir(), m.cfg)
	}
}

func (m Model) modelPickerActive() bool { return m.models.active }
