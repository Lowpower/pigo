package runtime

import (
	"context"
	"strings"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/ext"
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
	return e.commitActive(names, false, true)
}

// commitActive replaces the provider-facing set.
// force writes the session even when the in-memory set did not change, which
// is how a resume drops a tool that never made it into memory.
// hold keeps unresolved mcp names from names so the next prompt can retry them.
func (e *Engine) commitActive(names []string, force, hold bool) []string {
	e.mu.Lock()
	changed, added := e.applyActiveToolsLocked(names)
	if hold {
		e.notePendingLocked(names)
	} else {
		e.pendingTools = nil
	}
	snapshot := e.activeSnapshotLocked()
	sess := e.Opts.Session
	e.mu.Unlock()
	if (changed || force) && sess != nil {
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
		if !registered[n] {
			continue
		}
		switch e.exposureOf(n) {
		case ext.ExposureHidden:
			continue
		case ext.ExposureDeferred, ext.ExposureCodeMode:
			e.markLoadedLocked(n)
		}
		want[n] = true
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
	e.notePendingLocked(names)
}

// notePendingLocked remembers mcp names that are not registered yet, or that
// are deferred or codemode and have not been loaded. The caller holds e.mu.
func (e *Engine) notePendingLocked(wanted []string) {
	have := map[string]bool{}
	for _, name := range e.activeSnapshotLocked() {
		have[name] = true
	}
	var pending []string
	seen := map[string]bool{}
	for _, name := range wanted {
		if name == "" || seen[name] || !strings.HasPrefix(name, "mcp__") {
			continue
		}
		seen[name] = true
		if e.mcpUnresolvedLocked(name, have[name]) {
			pending = append(pending, name)
		}
	}
	e.pendingTools = pending
}

func (e *Engine) mcpUnresolvedLocked(name string, active bool) bool {
	if e.Tools == nil || !e.Tools.Has(name) {
		return true
	}
	switch e.exposureOf(name) {
	case ext.ExposureDeferred, ext.ExposureCodeMode:
		return e.loadedDeferred == nil || !e.loadedDeferred[name]
	default:
		return !active
	}
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
	return e.applyTurn(e.durableTools())
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

// settleLoadedTools waits for servers that own pending mcp names, then keeps
// the tools that came back and drops the rest.
func (e *Engine) settleLoadedTools(ctx context.Context) {
	if e == nil {
		return
	}
	e.mu.Lock()
	pending := append([]string(nil), e.pendingTools...)
	e.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	if e.mcp != nil {
		_ = e.mcp.WaitRefs(ctx, pending)
	}
	e.installMCPTools()

	e.mu.Lock()
	current := e.activeSnapshotLocked()
	e.mu.Unlock()
	drop := map[string]bool{}
	var keep []string
	for _, name := range pending {
		if e.Tools != nil && e.Tools.Has(name) {
			keep = append(keep, name)
			continue
		}
		drop[name] = true
	}
	next := make([]string, 0, len(current)+len(keep))
	seen := map[string]bool{}
	for _, name := range current {
		if drop[name] || seen[name] {
			continue
		}
		seen[name] = true
		next = append(next, name)
	}
	for _, name := range keep {
		if seen[name] {
			continue
		}
		seen[name] = true
		next = append(next, name)
	}
	e.commitActive(next, len(drop) > 0, false)
}
