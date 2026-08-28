package tui

type enabledIDs struct {
	all bool
	ids []string
}

func (e enabledIDs) clone() enabledIDs {
	if e.all {
		return enabledIDs{all: true}
	}
	return enabledIDs{ids: append([]string(nil), e.ids...)}
}

func (e enabledIDs) isEnabled(id string) bool {
	if e.all {
		return true
	}
	for _, x := range e.ids {
		if x == id {
			return true
		}
	}
	return false
}

func toggleID(e enabledIDs, id string) enabledIDs {
	if e.all {
		return enabledIDs{ids: []string{id}}
	}
	out := make([]string, 0, len(e.ids)+1)
	found := false
	for _, x := range e.ids {
		if x == id {
			found = true
			continue
		}
		out = append(out, x)
	}
	if !found {
		out = append(out, id)
	}
	return enabledIDs{ids: out}
}

func enableAllIDs(e enabledIDs, allIDs, targetIDs []string) enabledIDs {
	if e.all {
		return enabledIDs{all: true}
	}
	targets := targetIDs
	if targets == nil {
		targets = allIDs
	}
	out := append([]string(nil), e.ids...)
	have := map[string]bool{}
	for _, x := range out {
		have[x] = true
	}
	for _, id := range targets {
		if !have[id] {
			out = append(out, id)
			have[id] = true
		}
	}
	if len(out) == len(allIDs) {
		ok := true
		allow := map[string]bool{}
		for _, id := range allIDs {
			allow[id] = true
		}
		for _, id := range out {
			if !allow[id] {
				ok = false
				break
			}
		}
		if ok {
			return enabledIDs{all: true}
		}
	}
	return enabledIDs{ids: out}
}

func clearAllIDs(e enabledIDs, allIDs, targetIDs []string) enabledIDs {
	if e.all {
		if targetIDs == nil {
			return enabledIDs{ids: []string{}}
		}
		drop := map[string]bool{}
		for _, id := range targetIDs {
			drop[id] = true
		}
		var out []string
		for _, id := range allIDs {
			if !drop[id] {
				out = append(out, id)
			}
		}
		if out == nil {
			out = []string{}
		}
		return enabledIDs{ids: out}
	}
	drop := map[string]bool{}
	if targetIDs == nil {
		for _, id := range e.ids {
			drop[id] = true
		}
	} else {
		for _, id := range targetIDs {
			drop[id] = true
		}
	}
	var out []string
	for _, id := range e.ids {
		if !drop[id] {
			out = append(out, id)
		}
	}
	if out == nil {
		out = []string{}
	}
	return enabledIDs{ids: out}
}

func moveID(e enabledIDs, id string, delta int) enabledIDs {
	if e.all {
		return enabledIDs{all: true}
	}
	list := append([]string(nil), e.ids...)
	idx := -1
	for i, x := range list {
		if x == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return enabledIDs{ids: list}
	}
	j := idx + delta
	if j < 0 || j >= len(list) {
		return enabledIDs{ids: list}
	}
	list[idx], list[j] = list[j], list[idx]
	return enabledIDs{ids: list}
}

func sortedIDs(e enabledIDs, allIDs []string) []string {
	if e.all {
		return append([]string(nil), allIDs...)
	}
	have := map[string]bool{}
	for _, id := range e.ids {
		have[id] = true
	}
	out := append([]string(nil), e.ids...)
	for _, id := range allIDs {
		if !have[id] {
			out = append(out, id)
		}
	}
	return out
}

func sessionScopeIDs(e enabledIDs, available []string) (ids []string, implicitAll bool) {
	if e.all {
		return nil, true
	}
	allow := map[string]bool{}
	for _, id := range available {
		allow[id] = true
	}
	hasAvailable := false
	for _, id := range e.ids {
		if allow[id] {
			hasAvailable = true
			break
		}
	}
	allChecked := true
	for _, id := range available {
		if !e.isEnabled(id) {
			allChecked = false
			break
		}
	}
	if !hasAvailable || allChecked {
		return nil, true
	}
	return append([]string(nil), e.ids...), false
}
