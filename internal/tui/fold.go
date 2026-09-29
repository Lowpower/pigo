package tui

import (
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/Lowpower/pigo/internal/compaction"
)

// foldUserEntry recognizes a user-turn payload that should render as a
// collapsible title. The session text is unchanged; only the view differs.
func foldUserEntry(text string) (entry, bool) {
	if name, body, ok := parseSkillBlock(text); ok {
		return entry{
			role:      "skill",
			foldID:    foldKey("skill", text),
			foldKind:  "skill",
			foldTitle: "[skill] " + name,
			foldBody:  body,
			rendered:  text,
		}, true
	}
	if body, ok := unwrapFold(text, compaction.SummaryPrefix, compaction.SummarySuffix); ok {
		return entry{
			role:      "compaction",
			foldID:    foldKey("compaction", text),
			foldKind:  "compaction",
			foldTitle: "[compaction]",
			foldBody:  body,
			rendered:  text,
		}, true
	}
	if body, ok := unwrapFold(text, compaction.BranchSummaryPrefix, compaction.BranchSummarySuffix); ok {
		return entry{
			role:      "branch",
			foldID:    foldKey("branch", text),
			foldKind:  "branch",
			foldTitle: "[branch]",
			foldBody:  body,
			rendered:  text,
		}, true
	}
	return entry{}, false
}

func unwrapFold(text, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, suffix) {
		return "", false
	}
	if len(text) < len(prefix)+len(suffix) {
		return "", false
	}
	return text[len(prefix) : len(text)-len(suffix)], true
}

func parseSkillBlock(s string) (name, body string, ok bool) {
	const open = `<skill name="`
	if !strings.HasPrefix(s, open) || !strings.HasSuffix(s, "\n</skill>") {
		return "", "", false
	}
	rest := strings.TrimPrefix(s, open)
	name, rest, ok = strings.Cut(rest, `" location="`)
	if !ok || name == "" {
		return "", "", false
	}
	_, rest, ok = strings.Cut(rest, "\">\n")
	if !ok {
		return "", "", false
	}
	body = strings.TrimSuffix(rest, "\n</skill>")
	return name, body, true
}

func foldKey(kind, text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(text))
	return kind + ":" + strconv.FormatUint(h.Sum64(), 16)
}

func (m Model) foldRendered(e entry) string {
	title := m.metaStyle.Render(e.foldTitle)
	if !m.blockOpen[e.foldID] || e.foldBody == "" {
		return title
	}
	return title + "\n" + e.foldBody
}

func (m *Model) toggleFold(id string) {
	if id == "" {
		return
	}
	if m.blockOpen == nil {
		m.blockOpen = map[string]bool{}
	}
	m.blockOpen[id] = !m.blockOpen[id]
}

// titleAt reports the fold id whose title occupies screen row y in the
// fullscreen transcript. The dock, search bar, and non-fullscreen mode miss.
func (m Model) titleAt(y int) string {
	if !m.altScreen || m.height <= 0 || y < 0 {
		return ""
	}
	dockH := lineCount(strings.TrimRight(m.viewDock(), "\n"))
	if dockH >= m.height {
		return ""
	}
	bodyH := m.height - dockH
	if y >= bodyH {
		return ""
	}
	if m.searchActive && y == bodyH-1 {
		return ""
	}
	raw, titles := m.rawChatBody()
	body, hits := wrapWithTitleHits(raw, titles, m.transcriptWidth())
	if n := m.cfg.OutputPadN(); n > 0 {
		body = padLines(body, n)
	}
	_, start, total := clipWindowLines(body, bodyH, m.scrollOff)
	if total <= bodyH {
		start = 0
	}
	return hits[start+y]
}

func wrapWithTitleHits(raw string, titles map[int]string, width int) (string, map[int]string) {
	if raw == "" {
		return "", nil
	}
	lines := strings.Split(raw, "\n")
	hits := map[int]string{}
	n := 0
	for i, line := range lines {
		wrapped := wrapDisplay(line, width)
		parts := strings.Split(wrapped, "\n")
		id := ""
		if titles != nil {
			id = titles[i]
		}
		for range parts {
			if id != "" {
				hits[n] = id
			}
			n++
		}
	}
	return wrapDisplayBlock(raw, width), hits
}
