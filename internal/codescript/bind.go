package codescript

import (
	"strings"
	"unicode"

	"github.com/Lowpower/pigo/internal/ext"
)

// Tool is one call the script may make. Exposure uses the ext package values.
type Tool struct {
	Name         string
	Description  string
	Schema       map[string]any
	OutputSchema map[string]any
	Exposure     string
	Namespace    string
	Aliases      []string
}

// Namespace is a group of tools, such as one MCP server.
// Aliases are extra names describeNamespace and searchTools accept.
type Namespace struct {
	Name         string
	Description  string
	Instructions string
	Aliases      []string
}

// ModelInfo is one catalog row models.getAvailableOfType returns.
type ModelInfo struct {
	Type          string   `json:"type"`
	Provider      string   `json:"provider"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	API           string   `json:"api"`
	Input         []string `json:"input"`
	ContextWindow int      `json:"contextWindow,omitempty"`
}

// callable tools are the ones installed on the tools object.
// direct tools must be in the session set. codemode and deferred tools stay
// callable even when the model list does not include them. hidden, model-only,
// and the codemode tool itself are left out.
func callable(env Env) []Tool {
	active := map[string]bool{}
	for _, name := range env.Active {
		active[name] = true
	}
	var out []Tool
	seen := map[string]bool{}
	for _, t := range env.Tools {
		if t.Name == "" || t.Name == "codemode" {
			continue
		}
		id := jsName(t.Name)
		if seen[id] {
			continue
		}
		switch ext.NormalizeExposure(t.Exposure) {
		case ext.ExposureHidden, ext.ExposureModelOnly:
			continue
		case ext.ExposureCodeMode, ext.ExposureDeferred:
		default:
			if env.ActiveSet && !active[t.Name] {
				continue
			}
		}
		seen[id] = true
		out = append(out, t)
	}
	return out
}

// jsName is a tool's property on tools. Characters that cannot appear in a
// JavaScript identifier become underscores.
func jsName(name string) string {
	var b strings.Builder
	for i, r := range name {
		ok := r == '_' || r == '$' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r))
		if ok {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

func suggest(name string, tools []Tool) string {
	want := strings.ToLower(jsName(name))
	var exact []string
	var near []string
	seen := map[string]bool{}
	for _, t := range tools {
		cand := jsName(t.Name)
		if seen[cand] {
			continue
		}
		seen[cand] = true
		fold := strings.ToLower(cand)
		if fold == want {
			exact = append(exact, cand)
			continue
		}
		if editDistance(fold, want) <= 2 {
			near = append(near, cand)
		}
	}
	hits := exact
	if len(hits) == 0 {
		hits = near
	}
	if len(hits) > 3 {
		hits = hits[:3]
	}
	msg := "tools." + name + " does not exist"
	if len(hits) == 0 {
		return msg
	}
	msg += ". Did you mean tools." + hits[0]
	for _, hit := range hits[1:] {
		msg += " or tools." + hit
	}
	return msg + "?"
}

func editDistance(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) < len(br) {
		ar, br = br, ar
	}
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur := make([]int, len(br)+1)
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := cur[j-1] + 1
			sub := prev[j-1] + cost
			cur[j] = del
			if ins < cur[j] {
				cur[j] = ins
			}
			if sub < cur[j] {
				cur[j] = sub
			}
		}
		prev = cur
	}
	return prev[len(br)]
}

func normNS(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "mcp__")
	return strings.ReplaceAll(s, "-", "_")
}

type nsGroup struct {
	Name         string
	Description  string
	Instructions string
	Aliases      []string
	tools        []Tool
}

func collectNamespaces(env Env, tools []Tool) []nsGroup {
	index := map[string]int{}
	var groups []nsGroup
	add := func(name string) *nsGroup {
		key := normNS(name)
		if key == "" {
			return nil
		}
		if i, ok := index[key]; ok {
			return &groups[i]
		}
		index[key] = len(groups)
		groups = append(groups, nsGroup{Name: name})
		return &groups[len(groups)-1]
	}
	for _, ns := range env.Namespaces {
		g := add(ns.Name)
		if g == nil {
			continue
		}
		if g.Description == "" {
			g.Description = ns.Description
		}
		if g.Instructions == "" {
			g.Instructions = ns.Instructions
		}
		g.Aliases = append(g.Aliases, ns.Aliases...)
	}
	for _, t := range tools {
		if t.Namespace == "" {
			continue
		}
		if g := add(t.Namespace); g != nil {
			g.tools = append(g.tools, t)
		}
	}
	return groups
}

func findNamespace(groups []nsGroup, query string) *nsGroup {
	key := normNS(query)
	if key == "" {
		return nil
	}
	for i := range groups {
		g := &groups[i]
		if normNS(g.Name) == key {
			return g
		}
		for _, alias := range g.Aliases {
			if normNS(alias) == key {
				return g
			}
		}
	}
	return nil
}
