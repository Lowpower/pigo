package models

import "strings"

// Model is a known catalog entry used by --list-models and /model.
type Model struct {
	Provider string
	ID       string
	API      string
}

// Catalog is the built-in subset pigo can actually drive today.
func Catalog() []Model {
	return []Model{
		{Provider: "anthropic", ID: "claude-sonnet-4", API: "anthropic-messages"},
		{Provider: "anthropic", ID: "claude-opus-4", API: "anthropic-messages"},
		{Provider: "anthropic", ID: "claude-haiku-4", API: "anthropic-messages"},
		{Provider: "openai", ID: "gpt-4o", API: "openai-completions"},
		{Provider: "openai", ID: "gpt-4.1", API: "openai-completions"},
		{Provider: "opencode", ID: "claude-sonnet-4", API: "opencode"},
		{Provider: "opencode", ID: "gpt-4o", API: "opencode"},
	}
}

// Search filters the catalog by a substring of provider/id.
func Search(q string) []Model {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return Catalog()
	}
	var out []Model
	for _, m := range Catalog() {
		hay := strings.ToLower(m.Provider + "/" + m.ID + " " + m.API)
		if strings.Contains(hay, q) {
			out = append(out, m)
		}
	}
	return out
}

func (m Model) String() string { return m.Provider + "/" + m.ID }
