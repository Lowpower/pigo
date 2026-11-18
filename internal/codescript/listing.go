package codescript

import (
	"strings"
	"unicode/utf8"
)

// JSName is the tools.<name> identifier for a tool name.
func JSName(name string) string { return jsName(name) }

// Callable is the tool list installed on the tools object.
func Callable(env Env) []Tool { return callable(env) }

// FormatListing renders tool declarations that fit in budget.
// budget is estimated tokens, counting four characters as one token.
// Zero or less lists namespace names and no declarations.
func FormatListing(tools []Tool, budget int) string {
	if len(tools) == 0 {
		return ""
	}
	if budget <= 0 {
		return namespaceList(tools)
	}
	type group struct {
		name  string
		tools []Tool
	}
	var groups []group
	index := map[string]int{}
	for _, t := range tools {
		key := t.Namespace
		i, ok := index[key]
		if !ok {
			i = len(groups)
			index[key] = i
			groups = append(groups, group{name: key})
		}
		groups[i].tools = append(groups[i].tools, t)
	}
	var b strings.Builder
	used := 0
	for _, g := range groups {
		var section strings.Builder
		header := ""
		if g.name != "" {
			header = g.name + "\n"
		}
		wrote := false
		for _, t := range g.tools {
			block := t.Name
			if t.Description != "" {
				block += ": " + t.Description
			}
			block += "\n" + Declaration(t)
			extra := block
			if !wrote && header != "" {
				extra = header + block
			}
			cost := tokenCost(extra)
			if used+cost > budget {
				continue
			}
			if !wrote && header != "" {
				section.WriteString(header)
			}
			if wrote {
				section.WriteByte('\n')
			}
			section.WriteString(block)
			wrote = true
			used += cost
		}
		if !wrote {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(section.String())
	}
	return b.String()
}

func namespaceList(tools []Tool) string {
	seen := map[string]bool{}
	var names []string
	for _, t := range tools {
		if t.Namespace == "" || seen[t.Namespace] {
			continue
		}
		seen[t.Namespace] = true
		names = append(names, t.Namespace)
	}
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Namespaces:")
	for _, name := range names {
		b.WriteString("\n- ")
		b.WriteString(name)
	}
	return b.String()
}

func tokenCost(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	if c := n / 4; c > 0 {
		return c
	}
	return 1
}
