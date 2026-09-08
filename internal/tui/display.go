package tui

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

func osc8(text, href string) string {
	return "\x1b]8;;" + href + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

func fileURL(abs string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

func (m Model) linkPath(display, raw string) string {
	if display == "" || raw == "" || !m.cfg.HyperlinksEnabled(true) {
		return display
	}
	abs := raw
	if !filepath.IsAbs(abs) {
		base := m.cwd()
		if base != "" {
			abs = filepath.Join(base, raw)
		}
	}
	if !filepath.IsAbs(abs) {
		return display
	}
	return osc8(display, fileURL(abs))
}

func (m Model) present(s string) string {
	return m.finishView(m.withLayout(s))
}

func (m Model) withLayout(s string) string {
	if n := m.cfg.OutputPadN(); n > 0 {
		s = padLines(s, n)
	}
	if m.altScreen && m.height > 0 {
		s = m.clipRegion(s, m.height)
	}
	return s
}

func (m Model) finishView(s string) string {
	s = m.withSelection(s)
	if m.cfg.TerminalProgress() {
		s = progressOSC(m.running || m.bashRunning) + s
	}
	return m.withClip(s)
}

func (m Model) clipRegion(s string, height int) string {
	if height <= 0 {
		return s
	}
	if m.cfg.ScrollbarEnabled() {
		s = clipWithScrollbar(s, height, m.scrollOff)
	} else {
		s = clipWindow(s, height, m.scrollOff)
	}
	if m.scrollOff > 0 {
		s = withJumpLatest(s)
	}
	if m.searchActive {
		s = withSearchBar(s, m.searchQuery, m.searchN, len(m.searchHits))
	}
	return s
}

func (m Model) layoutFullscreen(body, dock string) string {
	if n := m.cfg.OutputPadN(); n > 0 {
		body = padLines(body, n)
		dock = padLines(dock, n)
	}
	dock = strings.TrimRight(dock, "\n")
	dockH := lineCount(dock)
	var s string
	if dockH >= m.height {
		s = m.clipRegion(joinBodyDock(body, dock), m.height)
	} else {
		body = m.clipRegion(body, m.height-dockH)
		s = joinBodyDock(body, dock)
	}
	return m.finishView(s)
}

func joinBodyDock(body, dock string) string {
	bodyLines := strings.Split(body, "\n")
	if dock == "" {
		return strings.Join(bodyLines, "\n")
	}
	return strings.Join(append(bodyLines, strings.Split(dock, "\n")...), "\n")
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func padLines(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}

func progressOSC(running bool) string {
	if running {
		return "\x1b]9;4;1;50\x1b\\"
	}
	return "\x1b]9;4;0\x1b\\"
}

func clipWindow(s string, height, offsetFromBottom int) string {
	if height <= 0 {
		return s
	}
	lines, _, total := clipWindowLines(s, height, offsetFromBottom)
	if total < height {
		lines = append(lines, make([]string, height-total)...)
	}
	return strings.Join(lines, "\n")
}

func clipWindowLines(s string, height, offsetFromBottom int) (vis []string, start, total int) {
	lines := strings.Split(s, "\n")
	total = len(lines)
	if total <= height {
		return lines, 0, total
	}
	maxOff := total - height
	if offsetFromBottom < 0 {
		offsetFromBottom = 0
	}
	if offsetFromBottom > maxOff {
		offsetFromBottom = maxOff
	}
	start = total - height - offsetFromBottom
	return lines[start : start+height], start, total
}

func clipWithScrollbar(s string, height, offsetFromBottom int) string {
	if height <= 0 {
		return s
	}
	vis, start, total := clipWindowLines(s, height, offsetFromBottom)
	if total <= height {
		if total < height {
			vis = append(vis, make([]string, height-total)...)
		}
		return strings.Join(vis, "\n")
	}
	thumb := start * (len(vis) - 1) / max(1, total-1)
	if thumb < 0 {
		thumb = 0
	}
	if thumb >= len(vis) {
		thumb = len(vis) - 1
	}
	vis[thumb] += " ▐"
	return strings.Join(vis, "\n")
}

func withJumpLatest(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return "↓ Jump to latest"
	}
	lines[len(lines)-1] = "↓ Jump to latest"
	return strings.Join(lines, "\n")
}

func withSearchBar(s, query string, idx, n int) string {
	label := "search: " + query
	if n > 0 {
		label += "  " + itoa(idx+1) + "/" + itoa(n)
	}
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return label
	}
	if len(lines) == 1 {
		return lines[0] + "\n" + label
	}
	lines[len(lines)-1] = label
	return strings.Join(lines, "\n")
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func indentMarkdownCodeBlocks(src, indent string) string {
	if indent == "" || indent == "  " {
		return src
	}
	lines := strings.Split(src, "\n")
	inFence := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			lines[i] = indent + strings.TrimPrefix(line, "  ")
		}
	}
	return strings.Join(lines, "\n")
}
