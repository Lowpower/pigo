package runtime

import (
	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/session"
)

func (e *Engine) bindExtensionHooks() {
	for _, h := range e.Hosts {
		if h == nil {
			continue
		}
		h.SetActiveToolsHook(func(names []string) {
			e.SetActiveTools(names)
		})
		host := h
		h.SetHostCall(func(name string, args map[string]any) map[string]any {
			return e.HandleHostCall(host, name, args)
		})
		h.SetShutdownRequest(func() {
			e.requestShutdown()
		})
	}
}

// SetActiveTools replaces the provider-facing active tool set. Names that are
// not registered are ignored. A purely additive change returns the newly
// added names; a shrink or replace returns nil.
func (e *Engine) SetActiveTools(names []string) []string {
	e.mu.Lock()
	changed, added := e.applyActiveToolsLocked(names)
	snapshot := e.activeSnapshotLocked()
	sess := e.Opts.Session
	e.mu.Unlock()
	if changed && sess != nil {
		_, _ = sess.AppendActiveToolsChange(snapshot)
		e.syncToolsIfDeclared()
	}
	return added
}

func (e *Engine) applyActiveToolsLocked(names []string) (changed bool, added []string) {
	registered := map[string]bool{}
	var order []string
	if e.Tools != nil {
		for _, t := range e.Tools.List() {
			registered[t.Name()] = true
			order = append(order, t.Name())
		}
	}
	want := map[string]bool{}
	for _, n := range names {
		if registered[n] {
			want[n] = true
		}
	}
	next := make([]string, 0, len(want))
	for _, n := range order {
		if want[n] {
			next = append(next, n)
		}
	}
	prev := e.activeSnapshotLocked()
	changed = !stringSlicesEqual(prev, next)
	if isStringSubset(prev, next) {
		for _, n := range next {
			if !containsString(prev, n) {
				added = append(added, n)
			}
		}
	}
	e.activeToolNames = next
	return changed, added
}

func (e *Engine) restoreActiveTools(s *session.Manager) {
	if s == nil {
		return
	}
	names, ok := session.LastActiveToolNames(s.GetBranch(""))
	if !ok {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.applyActiveToolsLocked(names)
}

func (e *Engine) activeSnapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.activeSnapshotLocked()
}

func (e *Engine) activeSnapshotLocked() []string {
	if e.activeToolNames == nil {
		if e.Tools == nil {
			return nil
		}
		var names []string
		for _, t := range e.Tools.List() {
			names = append(names, t.Name())
		}
		return names
	}
	return append([]string(nil), e.activeToolNames...)
}

func (e *Engine) providerTools() []ai.Tool {
	if e.Tools == nil {
		return nil
	}
	e.mu.Lock()
	names := e.activeToolNames
	if names != nil {
		names = append([]string(nil), names...)
	}
	e.mu.Unlock()
	return e.Tools.AIToolsNamed(names)
}

func (e *Engine) noteAddedTools(callID string, names []string) {
	if callID == "" || len(names) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.addedByCall == nil {
		e.addedByCall = map[string][]string{}
	}
	e.addedByCall[callID] = append([]string(nil), names...)
}

func (e *Engine) takeAddedTools(callID string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	names := e.addedByCall[callID]
	delete(e.addedByCall, callID)
	return names
}

func (e *Engine) toolAddedNames(call agent.ToolCall) []string {
	return e.takeAddedTools(call.ID)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isStringSubset(inner, outer []string) bool {
	if len(inner) == 0 {
		return true
	}
	have := make(map[string]bool, len(outer))
	for _, n := range outer {
		have[n] = true
	}
	for _, n := range inner {
		if !have[n] {
			return false
		}
	}
	return true
}

func containsString(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func addedNames(before, after []string) []string {
	if !isStringSubset(before, after) {
		return nil
	}
	var out []string
	for _, n := range after {
		if !containsString(before, n) {
			out = append(out, n)
		}
	}
	return out
}
