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
	if width <= 0 || s == "" || isImageProtocol(s) || keepOneLine(s) {
		return s
	}
	if strings.Contains(s, "\x1b]8;") {
		return wrapOSC8(s, width)
	}
	return wrap.String(s, width)
}

// wrapOSC8 wraps on visible cells and keeps OSC 8 sequences intact. A break
// inside an open link closes it and reopens it on the next line.
func wrapOSC8(s string, width int) string {
	var b strings.Builder
	col := 0
	open := ""
	for s != "" {
		if s[0] == 0x1b {
			n := skipEscape(s)
			if n < 1 {
				n = 1
			}
			seq := s[:n]
			if _, closed, ok := osc8State(seq); ok {
				if closed {
					open = ""
				} else {
					open = seq
				}
			}
			b.WriteString(seq)
			s = s[n:]
			continue
		}
		if s[0] == '\n' {
			writeOSC8Break(&b, open)
			col = 0
			s = s[1:]
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		w := runewidth.RuneWidth(r)
		if col > 0 && col+w > width {
			writeOSC8Break(&b, open)
			col = 0
			if r == ' ' || r == '\t' {
				s = s[size:]
				continue
			}
		}
		b.WriteRune(r)
		col += w
		s = s[size:]
	}
	return b.String()
}

func writeOSC8Break(b *strings.Builder, open string) {
	if open != "" {
		b.WriteString("\x1b]8;;\x1b\\")
	}
	b.WriteByte('\n')
	if open != "" {
		b.WriteString(open)
	}
}

// osc8State reports whether seq is an OSC 8 open or close.
func osc8State(seq string) (uri string, closed bool, ok bool) {
	if !strings.HasPrefix(seq, "\x1b]8;") {
		return "", false, false
	}
	body := strings.TrimPrefix(seq, "\x1b]8;")
	body = strings.TrimSuffix(body, "\x1b\\")
	body = strings.TrimSuffix(body, "\a")
	_, uri, found := strings.Cut(body, ";")
	if !found {
		return "", false, false
	}
	return uri, uri == "", true
}

// keepOneLine leaves a line whose visible text is a single URL unwrapped.
// A hard newline in the middle of that URL makes the terminal open only the
// first visual row. Other lines, including ones that contain a file hyperlink,
// still wrap.
func keepOneLine(s string) bool {
	plain := strings.TrimSpace(stripANSI(s))
	if plain == "" || strings.ContainsAny(plain, " \t\n") {
		return false
	}
	return strings.HasPrefix(plain, "https://") || strings.HasPrefix(plain, "http://")
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
	if m.altScreen && m.cfg.ScrollbarVisible(m.scrollOff) {
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
