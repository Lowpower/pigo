package runtime

import (
	"context"
	"strings"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/ext"
)

// selectModelTools is the model-facing name filter. A missing exposure is direct.
// activeSet false means no session whitelist. turnSet false means this turn
// does not narrow the list. An empty turn with turnSet true clears the list.
func selectModelTools(order []string, exposure map[string]string, loaded map[string]bool, active []string, activeSet bool, turn []string, turnSet bool) []string {
	eligible := make([]string, 0, len(order))
	for _, name := range order {
		if name == "" {
			continue
		}
		exp := ""
		if exposure != nil {
			exp = exposure[name]
		}
		if !ext.ModelVisible(exp, loaded[name]) {
			continue
		}
		eligible = append(eligible, name)
	}
	if activeSet {
		eligible = intersectNames(eligible, active)
	}
	if turnSet {
		eligible = intersectNames(eligible, turn)
	}
	return eligible
}

func intersectNames(order, allow []string) []string {
	have := make(map[string]bool, len(allow))
	for _, name := range allow {
		have[name] = true
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		if have[name] {
			out = append(out, name)
		}
	}
	return out
}

func (e *Engine) exposureOf(name string) string {
	info, ok := e.toolInfo(name)
	if !ok {
		return ext.ExposureDirect
	}
	return ext.NormalizeExposure(info.Exposure)
}

func (e *Engine) toolInfo(name string) (ext.ToolInfo, bool) {
	if e == nil {
		return ext.ToolInfo{}, false
	}
	var found ext.ToolInfo
	ok := false
	for _, h := range e.Hosts {
		if h == nil {
			continue
		}
		info, hit := h.ToolByName(name)
		if !hit {
			continue
		}
		found = info
		ok = true
	}
	return found, ok
}

// LookupTool finds a tool by name or alias. A real name wins over an alias.
func (e *Engine) LookupTool(name string) (ext.ToolInfo, bool) {
	if e == nil || name == "" {
		return ext.ToolInfo{}, false
	}
	var alias ext.ToolInfo
	haveAlias := false
	for _, h := range e.Hosts {
		if h == nil {
			continue
		}
		info, ok := h.Lookup(name)
		if !ok {
			continue
		}
		if info.Name == name {
			return info, true
		}
		if !haveAlias {
			alias = info
			haveAlias = true
		}
	}
	return alias, haveAlias
}

// ToolsInNamespace returns tools whose namespace is ns, in host order.
func (e *Engine) ToolsInNamespace(ns string) []ext.ToolInfo {
	if e == nil || ns == "" {
		return nil
	}
	var out []ext.ToolInfo
	for _, h := range e.Hosts {
		if h == nil {
			continue
		}
		for _, info := range h.ToolInfos() {
			if info.Namespace == ns {
				out = append(out, info)
			}
		}
	}
	return out
}

// ReachesTools returns the tools one call of name may reach.
// name may be an alias.
func (e *Engine) ReachesTools(name string) []string {
	info, ok := e.LookupTool(name)
	if !ok {
		return nil
	}
	return append([]string(nil), info.ReachesTools...)
}

// LoadDeferred marks deferred tools as loaded. Other names are ignored.
// Loaded tools stay out of the model list until the session whitelist includes
// them, or until there is no whitelist.
func (e *Engine) LoadDeferred(names []string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, name := range names {
		e.loadDeferredLocked(name)
	}
}

func (e *Engine) loadDeferredLocked(name string) {
	if e.exposureOf(name) != ext.ExposureDeferred {
		return
	}
	if e.loadedDeferred == nil {
		e.loadedDeferred = map[string]bool{}
	}
	e.loadedDeferred[name] = true
}

func (e *Engine) durableTools() []ai.Tool {
	if e == nil || e.Tools == nil {
		return nil
	}
	e.mu.Lock()
	activeSet := e.activeToolNames != nil
	active := append([]string(nil), e.activeToolNames...)
	loaded := map[string]bool{}
	for name, on := range e.loadedDeferred {
		if on {
			loaded[name] = true
		}
	}
	e.mu.Unlock()
	order := make([]string, 0)
	exposure := map[string]string{}
	for _, tool := range e.Tools.List() {
		order = append(order, tool.Name())
		if info, ok := e.toolInfo(tool.Name()); ok {
			exposure[tool.Name()] = info.Exposure
		}
	}
	names := selectModelTools(order, exposure, loaded, active, activeSet, nil, false)
	return e.decorate(e.Tools.AIToolsNamed(names))
}

func (e *Engine) applyTurn(tools []ai.Tool) []ai.Tool {
	if e == nil {
		return tools
	}
	e.mu.Lock()
	narrow := e.turnNarrow
	names := append([]string(nil), e.turnNames...)
	e.mu.Unlock()
	if !narrow {
		return tools
	}
	return filterAITools(tools, names)
}

func toolNamesOf(tools []ai.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func filterAITools(tools []ai.Tool, names []string) []ai.Tool {
	have := make(map[string]bool, len(names))
	for _, name := range names {
		have[name] = true
	}
	out := make([]ai.Tool, 0, len(tools))
	for _, tool := range tools {
		if have[tool.Name] {
			out = append(out, tool)
		}
	}
	return out
}

func (e *Engine) decorate(tools []ai.Tool) []ai.Tool {
	if e == nil || len(tools) == 0 {
		return tools
	}
	out := make([]ai.Tool, len(tools))
	for i, tool := range tools {
		if info, ok := e.toolInfo(tool.Name); ok {
			if info.Annotations != nil {
				tool.Annotations = info.Annotations
			}
			if info.OutputSchema != nil {
				tool.OutputSchema = info.OutputSchema
			}
		}
		out[i] = tool
	}
	return out
}

func (e *Engine) prepareTurnLoadout(ctx context.Context) {
	if e == nil {
		return
	}
	visible := toolNamesOf(e.durableTools())
	narrow := false
	allow := append([]string(nil), visible...)
	var guides []string
	for _, h := range e.Hosts {
		if h == nil || !h.PreparesLoadout() {
			continue
		}
		names, guidelines, constrained, err := h.PrepareLoadout(ctx, visible)
		if err != nil {
			continue
		}
		if constrained {
			narrow = true
			allow = intersectNames(allow, names)
		}
		if strings.TrimSpace(guidelines) != "" {
			guides = append(guides, strings.TrimSpace(guidelines))
		}
	}
	e.mu.Lock()
	e.turnNarrow = narrow
	e.turnNames = allow
	e.turnGuidelines = strings.Join(guides, "\n\n")
	e.mu.Unlock()
}

func (e *Engine) appendGuidelines(base string) string {
	if e == nil {
		return base
	}
	e.mu.Lock()
	g := strings.TrimSpace(e.turnGuidelines)
	e.mu.Unlock()
	if g == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return g
	}
	return base + "\n\n" + g
}

func (e *Engine) storeResultMeta(id string, meta agent.ResultMeta) {
	if id == "" {
		return
	}
	if meta.Usage == nil && meta.Nested == nil && len(meta.Structured) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.resultMeta == nil {
		e.resultMeta = map[string]agent.ResultMeta{}
	}
	e.resultMeta[id] = meta
}

func (e *Engine) takeResultMeta(call agent.ToolCall) agent.ResultMeta {
	if e == nil {
		return agent.ResultMeta{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	meta := e.resultMeta[call.ID]
	delete(e.resultMeta, call.ID)
	return meta
}
