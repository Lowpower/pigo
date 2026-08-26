package tui

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Lowpower/pigo/internal/session"
)

const (
	sortThreaded  = "threaded"
	sortRecent    = "recent"
	sortRelevance = "relevance"
	nameAll       = "all"
	nameNamed     = "named"
)

type pickerRow struct {
	session           session.Summary
	depth             int
	isLast            bool
	ancestorContinues []bool
}

type pickerQuery struct {
	regex *regexp.Regexp
	err   string
	raw   string
}

func parsePickerQuery(q string) pickerQuery {
	trimmed := strings.TrimSpace(q)
	if strings.HasPrefix(trimmed, "re:") {
		pat := strings.TrimSpace(trimmed[3:])
		if pat == "" {
			return pickerQuery{err: "Empty regex", raw: trimmed}
		}
		re, err := regexp.Compile("(?i)" + pat)
		if err != nil {
			return pickerQuery{err: err.Error(), raw: trimmed}
		}
		return pickerQuery{regex: re, raw: trimmed}
	}
	return pickerQuery{raw: trimmed}
}

func sessionSearchHay(s session.Summary) string {
	if s.SearchText != "" {
		return s.SearchText
	}
	return strings.Join([]string{s.ID, s.Name, s.FirstMessage, s.Cwd}, " ")
}

func matchSession(s session.Summary, q pickerQuery) (ok bool, score int) {
	hay := strings.ToLower(sessionSearchHay(s))
	if q.err != "" {
		return false, 0
	}
	if q.regex != nil {
		loc := q.regex.FindStringIndex(sessionSearchHay(s))
		if loc == nil {
			return false, 0
		}
		return true, loc[0]
	}
	raw := strings.ToLower(q.raw)
	if raw == "" {
		return true, 0
	}
	best := -1
	for _, tok := range strings.Fields(raw) {
		idx := strings.Index(hay, tok)
		if idx < 0 {
			return false, 0
		}
		if best < 0 || idx < best {
			best = idx
		}
	}
	name := strings.ToLower(s.Name)
	if name != "" && strings.Contains(name, raw) {
		best = max(0, best-100)
	}
	return true, best
}

func namedOnly(s session.Summary) bool {
	return strings.TrimSpace(s.Name) != ""
}

func applyNameFilter(src []session.Summary, nameFilter string) []session.Summary {
	if nameFilter != nameNamed {
		return src
	}
	var out []session.Summary
	for _, s := range src {
		if namedOnly(s) {
			out = append(out, s)
		}
	}
	return out
}

func filterPickerSessions(src []session.Summary, query, sortMode, nameFilter string) []pickerRow {
	named := applyNameFilter(src, nameFilter)
	q := parsePickerQuery(query)
	trimmed := strings.TrimSpace(query)

	if sortMode == sortThreaded && trimmed == "" {
		return flattenSessionSummaries(buildSessionSummaryTree(named))
	}

	type scored struct {
		s     session.Summary
		score int
	}
	var hit []scored
	for _, s := range named {
		ok, score := matchSession(s, q)
		if !ok {
			continue
		}
		hit = append(hit, scored{s: s, score: score})
	}
	if sortMode == sortRelevance && trimmed != "" {
		sort.SliceStable(hit, func(i, j int) bool {
			if hit[i].score != hit[j].score {
				return hit[i].score < hit[j].score
			}
			return hit[i].s.Modified.After(hit[j].s.Modified)
		})
	}
	out := make([]pickerRow, 0, len(hit))
	for _, h := range hit {
		out = append(out, pickerRow{session: h.s, isLast: true})
	}
	return out
}

type sessionTreeNode struct {
	session        session.Summary
	children       []*sessionTreeNode
	latestActivity int64
}

func buildSessionSummaryTree(sessions []session.Summary) []*sessionTreeNode {
	byPath := map[string]*sessionTreeNode{}
	for i := range sessions {
		s := sessions[i]
		p := filepath.Clean(s.Path)
		byPath[p] = &sessionTreeNode{session: s, latestActivity: s.Modified.UnixNano()}
	}
	var roots []*sessionTreeNode
	for i := range sessions {
		s := sessions[i]
		p := filepath.Clean(s.Path)
		node := byPath[p]
		parent := filepath.Clean(s.ParentSession)
		if parent != "." && parent != "" {
			if pn, ok := byPath[parent]; ok && pn != node {
				pn.children = append(pn.children, node)
				continue
			}
		}
		roots = append(roots, node)
	}
	var walkTime func(*sessionTreeNode) int64
	walkTime = func(n *sessionTreeNode) int64 {
		latest := n.latestActivity
		for _, c := range n.children {
			if t := walkTime(c); t > latest {
				latest = t
			}
		}
		n.latestActivity = latest
		return latest
	}
	for _, r := range roots {
		walkTime(r)
	}
	var sortNodes func([]*sessionTreeNode)
	sortNodes = func(nodes []*sessionTreeNode) {
		sort.SliceStable(nodes, func(i, j int) bool {
			return nodes[i].latestActivity > nodes[j].latestActivity
		})
		for _, n := range nodes {
			sortNodes(n.children)
		}
	}
	sortNodes(roots)
	return roots
}

func flattenSessionSummaries(roots []*sessionTreeNode) []pickerRow {
	var out []pickerRow
	var walk func(n *sessionTreeNode, depth int, ancestorContinues []bool, isLast bool)
	walk = func(n *sessionTreeNode, depth int, ancestorContinues []bool, isLast bool) {
		out = append(out, pickerRow{
			session:           n.session,
			depth:             depth,
			isLast:            isLast,
			ancestorContinues: ancestorContinues,
		})
		for i, c := range n.children {
			childLast := i == len(n.children)-1
			continues := false
			if depth > 0 {
				continues = !isLast
			}
			walk(c, depth+1, append(append([]bool{}, ancestorContinues...), continues), childLast)
		}
	}
	for i, r := range roots {
		walk(r, 0, nil, i == len(roots)-1)
	}
	return out
}

func cycleSortMode(cur string) string {
	switch cur {
	case sortThreaded:
		return sortRecent
	case sortRecent:
		return sortRelevance
	default:
		return sortThreaded
	}
}

func sortLabel(mode string) string {
	switch mode {
	case sortRecent:
		return "Recent"
	case sortRelevance:
		return "Relevance"
	default:
		return "Threaded"
	}
}

func pickerTreePrefix(row pickerRow) string {
	if row.depth == 0 {
		return ""
	}
	var b strings.Builder
	for _, cont := range row.ancestorContinues {
		if cont {
			b.WriteString("│ ")
		} else {
			b.WriteString("  ")
		}
	}
	if row.isLast {
		b.WriteString("└─")
	} else {
		b.WriteString("├─")
	}
	return b.String()
}
