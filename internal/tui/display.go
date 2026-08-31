package tui

import (
	"net/url"
	"path/filepath"
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
	return m.withClip(m.withLayout(s))
}

func (m Model) withLayout(s string) string {
	if n := m.cfg.OutputPadN(); n > 0 {
		s = padLines(s, n)
	}
	if m.altScreen && m.cfg.ScrollbarEnabled() && m.height > 0 {
		s = clipWithScrollbar(s, m.height)
	}
	if m.cfg.TerminalProgress() {
		s = progressOSC(m.running || m.bashRunning) + s
	}
	return s
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

func clipWithScrollbar(s string, height int) string {
	lines := strings.Split(s, "\n")
	total := len(lines)
	if total <= height {
		return s
	}
	start := total - height
	lines = lines[start:]
	thumb := (start + height - 1) * (len(lines) - 1) / max(1, total-1)
	if thumb < 0 {
		thumb = 0
	}
	if thumb >= len(lines) {
		thumb = len(lines) - 1
	}
	lines[thumb] += " ▐"
	return strings.Join(lines, "\n")
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
