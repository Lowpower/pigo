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
