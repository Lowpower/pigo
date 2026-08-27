package session

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// SortMode is the /resume list order.
type SortMode string

const (
	// SortThreaded groups sessions by parentSession (empty query only).
	SortThreaded SortMode = "threaded"
	// SortRecent keeps newest-first order.
	SortRecent SortMode = "recent"
	// SortFuzzy ranks by search score.
	SortFuzzy SortMode = "relevance"
)

// NameFilter limits the /resume list to named sessions.
type NameFilter string

const (
	// NameAll shows named and unnamed sessions.
	NameAll NameFilter = "all"
	// NameNamed shows only sessions with a display name.
	NameNamed NameFilter = "named"
)

// SearchToken is one query atom.
type SearchToken struct {
	Kind  string // fuzzy | phrase
	Value string
}

// ParsedQuery is a /resume search box.
type ParsedQuery struct {
	Mode   string // tokens | regex
	Tokens []SearchToken
	Regex  *regexp.Regexp
	Error  string
}

// ParseSearchQuery understands fuzzy tokens, "phrase", and re:<pattern>.
func ParseSearchQuery(query string) ParsedQuery {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ParsedQuery{Mode: "tokens"}
	}
	if strings.HasPrefix(trimmed, "re:") {
		pat := strings.TrimSpace(trimmed[3:])
		if pat == "" {
			return ParsedQuery{Mode: "regex", Error: "Empty regex"}
		}
		re, err := regexp.Compile("(?i)" + pat)
		if err != nil {
			return ParsedQuery{Mode: "regex", Error: err.Error()}
		}
		return ParsedQuery{Mode: "regex", Regex: re}
	}
	var tokens []SearchToken
	var buf strings.Builder
	inQuote := false
	flush := func(kind string) {
		v := strings.TrimSpace(buf.String())
		buf.Reset()
		if v == "" {
			return
		}
		tokens = append(tokens, SearchToken{Kind: kind, Value: v})
	}
	unclosed := false
	for _, ch := range trimmed {
		if ch == '"' {
			if inQuote {
				flush("phrase")
				inQuote = false
			} else {
				flush("fuzzy")
				inQuote = true
			}
			continue
		}
		if !inQuote && unicode.IsSpace(ch) {
			flush("fuzzy")
			continue
		}
		buf.WriteRune(ch)
	}
	if inQuote {
		unclosed = true
	}
	if unclosed {
		var plain []SearchToken
		for _, t := range strings.Fields(trimmed) {
			plain = append(plain, SearchToken{Kind: "fuzzy", Value: t})
		}
		return ParsedQuery{Mode: "tokens", Tokens: plain}
	}
	flush("fuzzy")
	return ParsedQuery{Mode: "tokens", Tokens: tokens}
}

func searchText(s Summary) string {
	if s.SearchText != "" {
		return s.SearchText
	}
	return strings.Join([]string{s.ID, s.Name, s.FirstMessage, s.Cwd}, " ")
}

func matchSession(s Summary, q ParsedQuery) (bool, int) {
	text := searchText(s)
	if q.Mode == "regex" {
		if q.Regex == nil {
			return false, 0
		}
		loc := q.Regex.FindStringIndex(text)
		if loc == nil {
			return false, 0
		}
		return true, loc[0]
	}
	if len(q.Tokens) == 0 {
		return true, 0
	}
	score := 0
	norm := ""
	for _, tok := range q.Tokens {
		if tok.Kind == "phrase" {
			if norm == "" {
				norm = collapseSpace(strings.ToLower(text))
			}
			phrase := collapseSpace(strings.ToLower(tok.Value))
			idx := strings.Index(norm, phrase)
			if idx < 0 {
				return false, 0
			}
			score += idx
			continue
		}
		ok, sc := subsequenceScore(strings.ToLower(tok.Value), strings.ToLower(text))
		if !ok {
			return false, 0
		}
		score += sc
	}
	return true, score
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func subsequenceScore(query, text string) (bool, int) {
	if query == "" {
		return true, 0
	}
	qi := 0
	score := 0
	last := -1
	consec := 0
	for i := 0; i < len(text) && qi < len(query); i++ {
		if text[i] != query[qi] {
			continue
		}
		if last == i-1 {
			consec++
			score -= consec * 5
		} else {
			consec = 0
			if last >= 0 {
				score += (i - last - 1) * 2
			}
		}
		last = i
		qi++
	}
	if qi < len(query) {
		return false, 0
	}
	return true, score
}

// FilterSessions applies name filter + search. sortMode "recent" keeps input order
// when filtering; "relevance"/threaded with a query sort by score then recency.
func FilterSessions(sessions []Summary, query string, sortMode SortMode, names NameFilter) []Summary {
	src := sessions
	if names == NameNamed {
		var named []Summary
		for _, s := range sessions {
			if strings.TrimSpace(s.Name) != "" {
				named = append(named, s)
			}
		}
		src = named
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return append([]Summary(nil), src...)
	}
	parsed := ParseSearchQuery(query)
	if parsed.Error != "" {
		return nil
	}
	if sortMode == SortRecent {
		var out []Summary
		for _, s := range src {
			if ok, _ := matchSession(s, parsed); ok {
				out = append(out, s)
			}
		}
		return out
	}
	type scored struct {
		s     Summary
		score int
	}
	var hit []scored
	for _, s := range src {
		ok, sc := matchSession(s, parsed)
		if !ok {
			continue
		}
		hit = append(hit, scored{s, sc})
	}
	sort.SliceStable(hit, func(i, j int) bool {
		if hit[i].score != hit[j].score {
			return hit[i].score < hit[j].score
		}
		return hit[i].s.Modified.After(hit[j].s.Modified)
	})
	out := make([]Summary, len(hit))
	for i, h := range hit {
		out[i] = h.s
	}
	return out
}

// ThreadedRow is one display line in threaded (empty-query) mode.
type ThreadedRow struct {
	Summary
	Prefix string
}

// BuildThread flattens parentSession trees. Roots and children are ordered by
// latest activity in each subtree (newest first).
func BuildThread(sessions []Summary) []ThreadedRow {
	byPath := map[string]int{}
	type node struct {
		s        Summary
		children []int
		latest   time.Time
	}
	nodes := make([]node, len(sessions))
	for i, s := range sessions {
		nodes[i] = node{s: s, latest: s.Modified}
		byPath[filepath.Clean(s.Path)] = i
	}
	var roots []int
	childOf := map[int]bool{}
	for i, s := range sessions {
		parent := filepath.Clean(s.ParentSession)
		if parent != "" && parent != "." {
			if pi, ok := byPath[parent]; ok && pi != i {
				nodes[pi].children = append(nodes[pi].children, i)
				childOf[i] = true
				continue
			}
		}
		roots = append(roots, i)
	}
	var bump func(int) time.Time
	bump = func(i int) time.Time {
		latest := nodes[i].latest
		for _, c := range nodes[i].children {
			t := bump(c)
			if t.After(latest) {
				latest = t
			}
		}
		nodes[i].latest = latest
		return latest
	}
	for _, r := range roots {
		bump(r)
	}
	var sortKids func([]int)
	sortKids = func(ids []int) {
		sort.SliceStable(ids, func(i, j int) bool {
			return nodes[ids[i]].latest.After(nodes[ids[j]].latest)
		})
		for _, id := range ids {
			sortKids(nodes[id].children)
		}
	}
	sortKids(roots)

	var out []ThreadedRow
	var walk func(i, depth int, ancestorCont []bool, last bool)
	walk = func(i, depth int, ancestorCont []bool, last bool) {
		prefix := ""
		if depth > 0 {
			var b strings.Builder
			for _, cont := range ancestorCont {
				if cont {
					b.WriteString("│  ")
				} else {
					b.WriteString("   ")
				}
			}
			if last {
				b.WriteString("└─ ")
			} else {
				b.WriteString("├─ ")
			}
			prefix = b.String()
		}
		out = append(out, ThreadedRow{Summary: nodes[i].s, Prefix: prefix})
		kids := nodes[i].children
		for k, c := range kids {
			cont := depth > 0 && !last
			walk(c, depth+1, append(append([]bool(nil), ancestorCont...), cont), k == len(kids)-1)
		}
	}
	for i, r := range roots {
		walk(r, 0, nil, i == len(roots)-1)
	}
	return out
}

// NextSort cycles threaded → recent → fuzzy → threaded.
func NextSort(cur SortMode) SortMode {
	switch cur {
	case SortThreaded:
		return SortRecent
	case SortRecent:
		return SortFuzzy
	default:
		return SortThreaded
	}
}

// FormatAge is a short relative timestamp (now/5m/2h/3d/1w/2mo/1y).
func FormatAge(modified, now time.Time) string {
	d := now.Sub(modified)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return itoa(int(d/time.Minute)) + "m"
	}
	if d < 24*time.Hour {
		return itoa(int(d/time.Hour)) + "h"
	}
	days := int(d / (24 * time.Hour))
	if days < 7 {
		return itoa(days) + "d"
	}
	if days < 30 {
		return itoa(days/7) + "w"
	}
	if days < 365 {
		return itoa(days/30) + "mo"
	}
	return itoa(days/365) + "y"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
