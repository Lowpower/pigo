package runtime

import (
	"strings"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/session"
)

// explicitToolSelection is true when the CLI replaces the enabled set instead
// of adjusting defaultTools: a plain --tools list, --no-tools, or
// --no-builtin-tools. Those runs do not restore a session's enabled tools.
func (e *Engine) explicitToolSelection() bool {
	if e == nil {
		return false
	}
	if e.Opts.NoTools || e.Opts.NoBuiltinTools {
		return true
	}
	return len(e.Opts.ToolAllow) > 0 && config.ToolModifiers(e.Opts.ToolAllow) == nil
}

// effectiveDefaultNames is the resolved defaultTools list with --tools
// +name/-name applied. Plain --tools lists are not part of this value.
func (e *Engine) effectiveDefaultNames() []string {
	base := e.Opts.Config.InitialBuiltinTools()
	if mods := config.ToolModifiers(e.Opts.ToolAllow); mods != nil {
		return config.ApplyToolModifiers(base, mods)
	}
	return base
}

// selectedActiveNames is the enabled set for a fresh registry: the resolved
// default plus model-visible extension tools, a plain --tools match, or the
// extension tools left by --no-builtin-tools. --exclude-tools is applied last.
func (e *Engine) selectedActiveNames() []string {
	if e == nil || e.Opts.NoTools || e.Tools == nil {
		return []string{}
	}
	registered := registeredNames(e)
	if e.Opts.NoBuiltinTools {
		return config.RejectToolNames(e.visibleNames(registered), e.Opts.ToolDeny)
	}
	if e.explicitToolSelection() {
		matched := selectExplicitTools(e.Opts.ToolAllow, registered)
		return config.RejectToolNames(matched, e.Opts.ToolDeny)
	}
	builtins := e.builtinNames()
	wanted := map[string]bool{}
	for _, name := range e.effectiveDefaultNames() {
		wanted[name] = true
	}
	var selected []string
	for _, name := range registered {
		if builtins[name] {
			if wanted[name] {
				selected = append(selected, name)
			}
			continue
		}
		if e.modelVisible(name) {
			selected = append(selected, name)
		}
	}
	return config.RejectToolNames(selected, e.Opts.ToolDeny)
}

// selectExplicitTools applies a plain --tools list. Built-in and extension
// names must match the list. MCP tools stay unless some entry starts with
// mcp__, in which case only the matching MCP tools stay.
func selectExplicitTools(allow, registered []string) []string {
	var patterns []string
	for _, item := range allow {
		if strings.HasPrefix(item, "mcp__") {
			patterns = append(patterns, item)
		}
	}
	var out []string
	for _, name := range registered {
		if strings.HasPrefix(name, "mcp__") {
			if len(patterns) == 0 || config.ToolMatchesAny(patterns, name) {
				out = append(out, name)
			}
			continue
		}
		if config.ToolMatchesAny(allow, name) {
			out = append(out, name)
		}
	}
	return out
}

func registeredNames(e *Engine) []string {
	if e == nil || e.Tools == nil {
		return nil
	}
	var names []string
	for _, tool := range e.Tools.List() {
		names = append(names, tool.Name())
	}
	return names
}

func (e *Engine) builtinNames() map[string]bool {
	reg := e.builtinRegistry()
	set := map[string]bool{}
	if reg == nil {
		return set
	}
	for _, tool := range reg.List() {
		set[tool.Name()] = true
	}
	return set
}

func (e *Engine) visibleNames(names []string) []string {
	var out []string
	for _, name := range names {
		if e.modelVisible(name) {
			out = append(out, name)
		}
	}
	return out
}

func (e *Engine) modelVisible(name string) bool {
	loaded := false
	e.mu.Lock()
	if e.loadedDeferred != nil {
		loaded = e.loadedDeferred[name]
	}
	e.mu.Unlock()
	return ext.ModelVisible(e.exposureOf(name), loaded)
}

// installActiveTools sets the session whitelist without writing a session entry.
// Unknown, hidden, and codemode names are dropped. An empty list enables nothing.
func (e *Engine) installActiveTools(names []string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.applyActiveToolsLocked(names)
}

// applySessionToolPolicy restores the session prompt and, unless the CLI asked
// for an explicit set, the session's enabled tools. --tools +name/-name is
// applied after that restore. --exclude-tools still drops matches.
func (e *Engine) applySessionToolPolicy(s *session.Manager) {
	if e == nil || s == nil {
		return
	}
	_, hasChange := session.LastActiveToolNames(s.GetBranch(""))
	st := session.ReplaySystem(s.GetBranch(""))
	if e.explicitToolSelection() {
		if st.Declared {
			e.System = st.Prompt
			e.sawPreamble = false
		}
		e.installActiveTools(e.selectedActiveNames())
		return
	}
	if hasChange {
		e.restoreActiveTools(s)
	}
	if st.Declared {
		e.restoreSystem(s)
	}
	if !hasChange && !st.Declared {
		return
	}
	mods := config.ToolModifiers(e.Opts.ToolAllow)
	if mods == nil && len(e.Opts.ToolDeny) == 0 {
		return
	}
	names := e.activeSnapshot()
	if mods != nil {
		names = config.ApplyToolModifiers(names, mods)
	}
	e.installActiveTools(config.RejectToolNames(names, e.Opts.ToolDeny))
}

// resetActiveToolsAfterReload keeps the current enabled set, then enables
// names that the resolved default gained. Tools removed from settings stay
// enabled. Tools the session turned off stay off unless the new default adds
// them. An explicit CLI selection is rebuilt from scratch. Extension tools
// that were not registered before this reload are enabled when they are
// model-visible.
func (e *Engine) resetActiveToolsAfterReload(prevActive []string, prevRegistered, prevDefault map[string]bool) {
	if e.explicitToolSelection() {
		e.installActiveTools(e.selectedActiveNames())
		return
	}
	var added []string
	for _, name := range e.effectiveDefaultNames() {
		if !prevDefault[name] {
			added = append(added, name)
		}
	}
	merged := append([]string{}, prevActive...)
	merged = append(merged, added...)
	for _, name := range registeredNames(e) {
		if prevRegistered[name] {
			continue
		}
		if e.modelVisible(name) {
			merged = append(merged, name)
		}
	}
	e.installActiveTools(config.RejectToolNames(merged, e.Opts.ToolDeny))
}
