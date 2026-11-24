// Package toolsearch ranks tools the model has not been shown yet.
// The ranker matches pi v1.3.0 tool_search: Okapi BM25 over tool metadata.
package toolsearch

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	// ToolName is the built-in tool the model calls.
	ToolName = "tool_search"
	// DefaultLimit is the number of matches returned when limit is omitted.
	DefaultLimit = 8
	// Description is fixed. It does not name servers, so connecting one does
	// not change the cached tool prefix.
	Description = "# Tool discovery\n\nSearches over deferred tool metadata with BM25 and exposes matching tools for the next model call.\n\nSome of the tools, such as tools of MCP servers, may not have been provided to you upfront, and you should use this tool (`tool_search`) to search for the required tools. For MCP tool discovery, always use `tool_search`."
)

// Meta is one searchable tool.
type Meta struct {
	Name                  string
	Description           string
	Schema                map[string]any
	NamespaceName         string
	NamespaceDescription  string
	NamespaceInstructions string
}

// Doc is the text the ranker scores.
type Doc struct {
	Name string
	Text string
}

// Hit is one ranked tool.
type Hit struct {
	Name  string
	Score float64
}

var stopWords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "by": {},
	"for": {}, "from": {}, "in": {}, "is": {}, "it": {}, "of": {}, "on": {}, "or": {},
	"that": {}, "the": {}, "this": {}, "to": {}, "with": {},
}

// Tokenize splits text the way pi v1.3.0 does: camelCase boundaries, lowercase,
// stop words removed, and a small plural stem.
func Tokenize(text string) []string {
	raw := tokenizeLower(strings.ToLower(splitCamel(text)))
	out := make([]string, 0, len(raw))
	for _, term := range raw {
		if _, stop := stopWords[term]; stop {
			continue
		}
		out = append(out, stem(term))
	}
	return out
}

func tokenizeLower(text string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, b.String())
		b.Reset()
	}
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func splitCamel(text string) string {
	var b strings.Builder
	runes := []rune(text)
	for i, r := range runes {
		if i > 0 && isCamelBoundary(runes, i) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isCamelBoundary(runes []rune, i int) bool {
	prev, cur := runes[i-1], runes[i]
	if isLowerOrDigit(prev) && isUpper(cur) {
		return true
	}
	if i+1 < len(runes) && isUpper(prev) && isUpper(cur) && isLower(runes[i+1]) {
		return true
	}
	return false
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
func isLower(r rune) bool { return r >= 'a' && r <= 'z' }
func isLowerOrDigit(r rune) bool {
	return isLower(r) || (r >= '0' && r <= '9')
}

func stem(term string) string {
	if len(term) > 4 && strings.HasSuffix(term, "ies") {
		return term[:len(term)-3] + "y"
	}
	if len(term) > 4 && hasStemSuffix(term) {
		return term[:len(term)-2]
	}
	if len(term) > 3 && strings.HasSuffix(term, "s") && !strings.HasSuffix(term, "ss") {
		return term[:len(term)-1]
	}
	return term
}

func hasStemSuffix(term string) bool {
	for _, suf := range []string{"ches", "shes", "sses", "xes", "zes"} {
		if strings.HasSuffix(term, suf) {
			return true
		}
	}
	return false
}

// Document builds the search text for one tool.
func Document(m Meta) Doc {
	parts := []string{m.Name, strings.ReplaceAll(m.Name, "_", " "), m.Description}
	parts = append(parts, schemaParts(m.Schema)...)
	if m.NamespaceName != "" || m.NamespaceDescription != "" || m.NamespaceInstructions != "" {
		parts = append(parts, m.NamespaceName, m.NamespaceDescription, m.NamespaceInstructions)
	}
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	return Doc{Name: m.Name, Text: strings.Join(kept, " ")}
}

func schemaParts(schema any) []string {
	var parts []string
	walkSchema(schema, &parts)
	return parts
}

func walkSchema(schema any, parts *[]string) {
	obj, ok := schema.(map[string]any)
	if !ok {
		return
	}
	if desc, ok := obj["description"].(string); ok {
		*parts = append(*parts, desc)
	}
	if props, ok := obj["properties"].(map[string]any); ok {
		for name, prop := range props {
			*parts = append(*parts, name)
			walkSchema(prop, parts)
		}
	}
	walkSchema(obj["items"], parts)
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		list, ok := obj[key].([]any)
		if !ok {
			continue
		}
		for _, item := range list {
			walkSchema(item, parts)
		}
	}
}

// Rank returns up to limit documents with a positive BM25 score, highest first.
// Equal scores keep document order. k1 is 1.2 and b is 0.75.
func Rank(query string, docs []Doc, limit int) []Hit {
	terms := unique(Tokenize(query))
	if len(terms) == 0 || len(docs) == 0 || limit <= 0 {
		return nil
	}
	counts := make([]map[string]int, len(docs))
	lengths := make([]int, len(docs))
	for i, doc := range docs {
		counts[i] = map[string]int{}
		for _, term := range Tokenize(doc.Text) {
			counts[i][term]++
			lengths[i]++
		}
	}
	var total int
	for _, n := range lengths {
		total += n
	}
	avg := float64(total) / float64(len(docs))
	if avg == 0 {
		avg = 1
	}
	idf := map[string]float64{}
	for _, term := range terms {
		df := 0
		for _, c := range counts {
			if c[term] > 0 {
				df++
			}
		}
		n := float64(len(docs))
		idf[term] = math.Log(1 + (n-float64(df)+0.5)/(float64(df)+0.5))
	}
	const k1 = 1.2
	const b = 0.75
	hits := make([]Hit, 0, len(docs))
	for i, doc := range docs {
		var score float64
		for _, term := range terms {
			count := counts[i][term]
			if count == 0 {
				continue
			}
			norm := k1 * (1 - b + (b*float64(lengths[i]))/avg)
			score += idf[term] * (float64(count) * (k1 + 1) / (float64(count) + norm))
		}
		if score > 0 {
			hits = append(hits, Hit{Name: doc.Name, Score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})
	if len(hits) == 0 {
		return nil
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func unique(terms []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, term := range terms {
		if seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
	}
	return out
}

// ParseLimit reads the optional limit argument.
// A missing value uses DefaultLimit. Anything that is not a positive integer
// is an error.
func ParseLimit(v any) (int, error) {
	if v == nil {
		return DefaultLimit, nil
	}
	n, ok := positiveInt(v)
	if !ok {
		return 0, ErrLimit
	}
	return n, nil
}

// ErrQuery and ErrLimit are the messages the tool returns for bad arguments.
var (
	ErrQuery = errString("query must not be empty")
	ErrLimit = errString("limit must be a positive integer")
)

type errString string

func (e errString) Error() string { return string(e) }

func positiveInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, n > 0
	case int32:
		return int(n), n > 0
	case int64:
		return int(n), n > 0
	case float64:
		if n != math.Trunc(n) || n <= 0 || n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

// FormatResult is the tool result text. descriptions maps a tool name to its
// full description; only the first line is shown.
func FormatResult(names []string, descriptions map[string]string) string {
	if len(names) == 0 {
		return "No matching tools found."
	}
	noun := "tools"
	if len(names) == 1 {
		noun = "tool"
	}
	var b strings.Builder
	b.WriteString("Loaded ")
	b.WriteString(strconv.Itoa(len(names)))
	b.WriteByte(' ')
	b.WriteString(noun)
	b.WriteString(". They are available from your next call:\n")
	for i, name := range names {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(firstLine(descriptions[name]))
	}
	return b.String()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
