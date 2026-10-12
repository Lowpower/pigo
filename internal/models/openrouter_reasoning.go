package models

// OpenRouterReasoning is the reasoning object on an OpenRouter model.
// supported_efforts null or empty means the gateway did not publish an allowlist.
type OpenRouterReasoning struct {
	Mandatory        bool     `json:"mandatory,omitempty"`
	SupportedEfforts []string `json:"supported_efforts,omitempty"`
}

// OpenRouterThinkingLevelMap converts OpenRouter reasoning metadata into a
// thinkingLevelMap. A nil result means the metadata does not restrict levels.
// Mandatory reasoning sets off to null so callers omit the effort field.
// Optional reasoning maps off to "none".
func OpenRouterThinkingLevelMap(meta OpenRouterReasoning) map[string]*string {
	if len(meta.SupportedEfforts) == 0 {
		if meta.Mandatory {
			return map[string]*string{"off": nil}
		}
		return nil
	}
	derived := effortThinkingLevelMap(meta.SupportedEfforts)
	if meta.Mandatory {
		derived["off"] = nil
	} else {
		none := "none"
		derived["off"] = &none
	}
	return derived
}

func applyOpenRouterReasoning(m Model, replaceMap bool) Model {
	if m.Provider != "openrouter" {
		return m
	}
	m = ensureOpenRouterFormat(m)
	meta, ok := lookupOpenRouterReasoning(m.ID)
	if !ok {
		return m
	}
	derived := OpenRouterThinkingLevelMap(meta)
	if derived == nil || (!replaceMap && len(m.ThinkingLevelMap) > 0) {
		return m
	}
	m.ThinkingLevelMap = mergeOpenRouterThinkingMap(m.ThinkingLevelMap, derived)
	return m
}

func ensureOpenRouterFormat(m Model) Model {
	if m.Provider != "openrouter" {
		return m
	}
	if m.Compat == nil {
		m.Compat = &Compat{ThinkingFormat: "openrouter"}
		return m
	}
	if m.Compat.ThinkingFormat != "" {
		return m
	}
	c := *m.Compat
	c.ThinkingFormat = "openrouter"
	m.Compat = &c
	return m
}

// mergeOpenRouterThinkingMap keeps an existing effort map when OpenRouter only
// says that off is unsupported. A published effort list replaces the map.
func mergeOpenRouterThinkingMap(base, derived map[string]*string) map[string]*string {
	if len(derived) == 1 {
		if _, onlyOff := derived["off"]; onlyOff && len(base) > 0 {
			out := make(map[string]*string, len(base)+1)
			for k, v := range base {
				out[k] = v
			}
			out["off"] = derived["off"]
			return out
		}
	}
	return derived
}
