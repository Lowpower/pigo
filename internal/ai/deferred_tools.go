package ai

import "strings"

// toolAnchorMode selects how late tools are attached to a transcript.
type toolAnchorMode int

const (
	toolAnchorOff toolAnchorMode = iota
	toolAnchorAdditional
	toolAnchorSearch
)

// deferredAnchor is the Responses input placement for tools that must stay
// out of the cached tool prefix.
type deferredAnchor struct {
	mode     toolAnchorMode
	deferred []Tool
}

// responsesDeferred splits tools for an OpenAI Responses request.
// The returned list is the top-level tools array. Late tools stay on anchor.
func responsesDeferred(reqCtx Context, opts Options) (deferredAnchor, []Tool) {
	mode := responsesAnchorMode(opts)
	if mode == toolAnchorOff {
		return deferredAnchor{}, reqCtx.Tools
	}
	immediate, deferred := splitDeferredTools(reqCtx.Messages, reqCtx.Tools, true)
	return deferredAnchor{mode: mode, deferred: deferred}, immediate
}

func responsesAnchorMode(opts Options) toolAnchorMode {
	c := lookupCompat(opts)
	if c == nil {
		return toolAnchorOff
	}
	if c.SupportsAdditionalTools {
		return toolAnchorAdditional
	}
	if c.SupportsToolSearch {
		return toolAnchorSearch
	}
	return toolAnchorOff
}

func chatDeferredKimi(opts Options) bool {
	if kimiCodingDeferredTools(opts.Provider, opts.Model) {
		return true
	}
	c := lookupCompat(opts)
	return c != nil && strings.EqualFold(strings.TrimSpace(c.DeferredToolsMode), "kimi")
}

// kimiCodingDeferredTools reports K3-style Kimi Coding models. Those ids accept
// tools added in a later system message. kimi-for-coding-highspeed does not.
func kimiCodingDeferredTools(provider, modelID string) bool {
	if !strings.EqualFold(strings.TrimSpace(provider), "kimi-coding") {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(modelID)) {
	case "k3", "k3-256k", "kimi-for-coding":
		return true
	default:
		return false
	}
}

// supportsToolReferences reports whether Anthropic Messages deferred-tool
// fields (defer_loading / tool_reference) should be sent.
func supportsToolReferences(opts Options) bool {
	if strings.EqualFold(strings.TrimSpace(opts.Provider), "fireworks") {
		return true
	}
	c := lookupCompat(opts)
	return c != nil && c.SupportsToolReferences
}

// splitDeferredTools partitions current tools into prefix (immediate) and
// transcript-loaded (deferred) definitions. Names not in tools are ignored.
// When every remaining tool would be deferred, all of them stay immediate so
// the request still has a tool prefix.
func splitDeferredTools(msgs []Message, tools []Tool, enabled bool) (immediate, deferred []Tool) {
	order := make([]string, 0, len(tools))
	byName := make(map[string]Tool, len(tools))
	for _, t := range tools {
		if _, ok := byName[t.Name]; !ok {
			order = append(order, t.Name)
		}
		byName[t.Name] = t
	}
	if !enabled {
		for _, name := range order {
			immediate = append(immediate, byName[name])
		}
		return immediate, nil
	}

	deferredNames := map[string]struct{}{}
	usedNames := map[string]struct{}{}
	for _, m := range msgs {
		if m.Assistant != nil {
			for _, b := range m.Assistant.Content {
				if b != nil && b.Type == KindToolCall && b.ToolName != "" {
					usedNames[b.ToolName] = struct{}{}
				}
			}
			continue
		}
		if m.Role != RoleToolResult && m.ToolCallID == "" {
			continue
		}
		for _, name := range m.AddedToolNames {
			if name == "" {
				continue
			}
			if _, used := usedNames[name]; !used {
				deferredNames[name] = struct{}{}
			}
		}
	}

	for _, name := range order {
		if _, ok := deferredNames[name]; ok {
			deferred = append(deferred, byName[name])
		} else {
			immediate = append(immediate, byName[name])
		}
	}
	if len(immediate) == 0 && len(deferred) > 0 {
		return deferred, nil
	}
	return immediate, deferred
}

// takeDeferredTools returns deferred tools named in names that have not been
// anchored yet, in names order. It records the ones it returns.
func takeDeferredTools(names []string, deferred []Tool, loaded map[string]struct{}) []Tool {
	if len(names) == 0 || len(deferred) == 0 {
		return nil
	}
	byName := make(map[string]Tool, len(deferred))
	for _, t := range deferred {
		byName[t.Name] = t
	}
	var out []Tool
	for _, name := range names {
		if name == "" {
			continue
		}
		t, ok := byName[name]
		if !ok {
			continue
		}
		if _, seen := loaded[name]; seen {
			continue
		}
		loaded[name] = struct{}{}
		out = append(out, t)
	}
	return out
}

func deferredNameSet(tools []Tool) map[string]struct{} {
	if len(tools) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		out[t.Name] = struct{}{}
	}
	return out
}
