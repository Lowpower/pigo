package ai

import "strings"

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
