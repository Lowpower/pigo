package mcp

import "strings"

// SectionLine is one mcp_servers row: the server name, how its tools are
// reached, and one summary line.
func SectionLine(name string, cfg Config, instructions string) string {
	phrases := reachPhrases(cfg)
	head := name + ": " + strings.Join(phrases, "; ")
	summary := Summary(cfg.Description, instructions)
	if summary == "" {
		return head
	}
	return head + ". " + summary
}

// Summary prefers the configured description, then the first line of the
// server instructions.
func Summary(description, instructions string) string {
	if line := firstLine(description); line != "" {
		return line
	}
	return firstLine(instructions)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func reachPhrases(cfg Config) []string {
	present := map[string]bool{cfg.ExposureValue(): true}
	for _, rule := range cfg.ToolExposure {
		present[normalizeExposure(rule.Exposure)] = true
	}
	order := []string{"direct", "model-only", "codemode", "deferred", "hidden"}
	seen := map[string]bool{}
	var phrases []string
	for _, exposure := range order {
		if !present[exposure] {
			continue
		}
		phrase := exposurePhrase(exposure)
		if seen[phrase] {
			continue
		}
		seen[phrase] = true
		phrases = append(phrases, phrase)
	}
	if len(phrases) == 0 {
		return []string{exposurePhrase(DefaultExposure)}
	}
	return phrases
}

func exposurePhrase(exposure string) string {
	switch exposure {
	case "direct", "model-only":
		return "declared to the model"
	case "deferred":
		return "loaded by tool_search, then called directly"
	case "hidden":
		return "unreachable"
	default:
		return "called from codemode scripts"
	}
}
