package models

import (
	"fmt"
	"sort"
	"strings"
)

// CLIModelRequest is one --model lookup against a catalog.
// Credentials only break ties. A unique match resolves even when nobody is authenticated.
type CLIModelRequest struct {
	// Text is the --model flag, including an optional :thinking suffix.
	Text string
	// Provider limits the search to one provider (--provider). Empty searches the whole list.
	Provider string
	// Authenticated providers currently have usable credentials.
	Authenticated []string
	// InlineAPIKey means --api-key was set. Stored credentials do not choose a provider.
	InlineAPIKey bool
	// Models is the catalog to search. Nil uses Catalog.
	Models []Model
}

// CLIModelResult is the catalog row selected for --model.
// Found is false when nothing matched; the caller keeps today's synthetic id.
type CLIModelResult struct {
	Model Model
	Found bool
}

// ResolveCLIModel selects one catalog model for a --model flag.
// Several providers offering the same id resolve to the single authenticated provider.
// Zero or several authenticated providers is an error that lists every provider/id.
// A provider/id prefix is only a hint: the whole string may be another provider's id.
func ResolveCLIModel(req CLIModelRequest) (CLIModelResult, error) {
	original := strings.TrimSpace(req.Text)
	text := stripThinkingSuffix(original)
	if text == "" {
		return CLIModelResult{}, nil
	}
	list := req.Models
	if list == nil {
		list = Catalog()
	}
	pool := list
	if constraint := strings.TrimSpace(req.Provider); constraint != "" {
		var filtered []Model
		for _, m := range list {
			if strings.EqualFold(m.Provider, constraint) {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) == 0 {
			return CLIModelResult{}, nil
		}
		pool = filtered
	}

	usable := map[string]bool{}
	if !req.InlineAPIKey {
		for _, id := range req.Authenticated {
			usable[strings.ToLower(id)] = true
		}
	}
	isUsable := func(provider string) bool {
		return usable[strings.ToLower(provider)]
	}

	lowerText := strings.ToLower(text)
	var raw []Model
	for _, m := range pool {
		if strings.ToLower(m.ID) == lowerText {
			raw = append(raw, m)
		}
	}
	named, hasNamed := namedModel(text, pool, raw)
	var others []Model
	for _, m := range raw {
		if hasNamed && m.Provider == named.Provider && m.ID == named.ID {
			continue
		}
		others = append(others, m)
	}
	candidates := others
	if hasNamed {
		candidates = append([]Model{named}, others...)
	}
	if len(candidates) == 0 {
		return CLIModelResult{}, nil
	}
	if oneProvider(candidates) {
		return CLIModelResult{Model: candidates[0], Found: true}, nil
	}
	if hasNamed && isUsable(named.Provider) {
		return CLIModelResult{Model: named, Found: true}, nil
	}
	var usableOthers []Model
	for _, m := range others {
		if isUsable(m.Provider) {
			usableOthers = append(usableOthers, m)
		}
	}
	if len(usableOthers) == 1 {
		return CLIModelResult{Model: usableOthers[0], Found: true}, nil
	}
	if len(usableOthers) == 0 && hasNamed {
		return CLIModelResult{Model: named, Found: true}, nil
	}
	return CLIModelResult{}, ambiguousCLIModel(original, candidates, len(usableOthers), req.InlineAPIKey)
}

// AmbiguousExactPatterns reports patterns whose exact model id is offered by
// more than one provider in list. Glob patterns stay on ResolvePatternsIn and
// do not warn here. The match set is not changed.
func AmbiguousExactPatterns(patterns []string, list []Model) []string {
	var out []string
	for _, raw := range patterns {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		pattern := stripThinkingSuffix(raw)
		if pattern == "" || strings.ContainsAny(pattern, "*?[]") {
			continue
		}
		seen := map[string]bool{}
		var names []string
		lower := strings.ToLower(pattern)
		for _, m := range list {
			if strings.ToLower(m.ID) != lower || seen[m.Provider] {
				continue
			}
			seen[m.Provider] = true
			names = append(names, m.Provider)
		}
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		out = append(out, fmt.Sprintf("Pattern %q matches several providers: %s. Use provider/id to choose one.", raw, strings.Join(names, ", ")))
	}
	return out
}

func stripThinkingSuffix(text string) string {
	i := strings.LastIndex(text, ":")
	if i < 0 || !IsThinkingLevel(text[i+1:]) {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(text[:i])
}

// namedModel is the provider/rest hint when the prefix is a provider in pool.
// The provider's own id wins. Otherwise a raw id equal to the whole text on that provider wins.
func namedModel(text string, pool, raw []Model) (Model, bool) {
	slash := strings.Index(text, "/")
	if slash < 0 {
		return Model{}, false
	}
	prefix := strings.TrimSpace(text[:slash])
	modelText := strings.TrimSpace(text[slash+1:])
	if prefix == "" || modelText == "" {
		return Model{}, false
	}
	var provider string
	for _, m := range pool {
		if strings.EqualFold(m.Provider, prefix) {
			provider = m.Provider
			break
		}
	}
	if provider == "" {
		return Model{}, false
	}
	lowerModel := strings.ToLower(modelText)
	for _, m := range pool {
		if m.Provider == provider && strings.ToLower(m.ID) == lowerModel {
			return m, true
		}
	}
	for _, m := range raw {
		if m.Provider == provider {
			return m, true
		}
	}
	return Model{}, false
}

func oneProvider(list []Model) bool {
	if len(list) == 0 {
		return false
	}
	first := list[0].Provider
	for _, m := range list[1:] {
		if m.Provider != first {
			return false
		}
	}
	return true
}

func ambiguousCLIModel(text string, candidates []Model, usableCount int, inline bool) error {
	seen := map[string]bool{}
	matches := make([]string, 0, len(candidates))
	for _, m := range candidates {
		label := m.Provider + "/" + m.ID
		if seen[label] {
			continue
		}
		seen[label] = true
		matches = append(matches, label)
	}
	sort.Strings(matches)
	hint := "More than one matching provider is authenticated."
	if inline {
		hint = "--api-key does not choose a provider."
	} else if usableCount == 0 {
		hint = "No matching provider is authenticated."
	}
	// Message text matches pi v1.3.0 resolveCliModel, including the capital and period.
	return fmt.Errorf("Model %q is ambiguous across providers: %s. %s Use --provider or provider/model.", text, strings.Join(matches, ", "), hint) //nolint:revive,staticcheck // pi CLI text
}
