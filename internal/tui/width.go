package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/muesli/reflow/wrap"
)

func isImageProtocol(s string) bool {
	return strings.HasPrefix(s, "\x1b_G") || strings.HasPrefix(s, "\x1b]1337;")
}

func wrapDisplay(s string, width int) string {
	if width <= 0 || s == "" || isImageProtocol(s) {
		return s
	}
	return wrap.String(s, width)
}

func wrapDisplayBlock(s string, width int) string {
	if width <= 0 || s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = wrapDisplay(line, width)
	}
	return strings.Join(lines, "\n")
}

func (m Model) transcriptWidth() int {
	w := m.width
	if w <= 0 {
		w = m.layoutWidth()
	}
	w -= m.cfg.OutputPadN()
	if m.altScreen && m.cfg.ScrollbarEnabled() {
		w -= 2
	}
	if w < 1 {
		return 1
	}
	return w
}

func visibleWidth(s string) int {
	n := 0
	for s != "" {
		if s[0] == 0x1b {
			s = s[skipEscape(s):]
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		n += runewidth.RuneWidth(r)
		s = s[size:]
	}
	return n
}

func truncateDisplay(s string, width int, tail string) string {
	if width <= 0 {
		return ""
	}
	if visibleWidth(s) <= width {
		return s
	}
	limit := width - visibleWidth(tail)
	if limit < 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	for s != "" {
		if s[0] == 0x1b {
			n := skipEscape(s)
			b.WriteString(s[:n])
			s = s[n:]
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		w := runewidth.RuneWidth(r)
		if used+w > limit {
			break
		}
		b.WriteString(s[:size])
		used += w
		s = s[size:]
	}
	b.WriteString(tail)
	return b.String()
}

func sliceByDisplay(s string, start, width int) string {
	if width <= 0 {
		return ""
	}
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	col := 0
	used := 0
	for s != "" {
		if s[0] == 0x1b {
			n := skipEscape(s)
			b.WriteString(s[:n])
			s = s[n:]
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		w := runewidth.RuneWidth(r)
		end := col + w
		if end <= start {
			col = end
			s = s[size:]
			continue
		}
		if col < start {
			// start lands inside a wide rune; skip the whole rune.
			col = end
			s = s[size:]
			continue
		}
		if used+w > width {
			break
		}
		b.WriteString(s[:size])
		used += w
		col = end
		s = s[size:]
	}
	return b.String()
}

func skipEscape(s string) int {
	if len(s) == 0 || s[0] != 0x1b {
		return 0
	}
	if len(s) == 1 {
		return 1
	}
	switch s[1] {
	case '[': // CSI
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
	case ']': // OSC (including OSC 8 hyperlinks)
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
	case 'P', 'X', '^', '_': // DCS / SOS / PM / APC (kitty)
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
	default:
		return 2
	}
	return len(s)
}
