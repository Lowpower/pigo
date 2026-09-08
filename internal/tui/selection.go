package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

type cellPos struct {
	X, Y int
}

type textSel struct {
	active   bool
	dragging bool
	dragged  bool
	ax, ay   int
	fx, fy   int
}

func (s textSel) has() bool {
	return s.active && (s.dragged || s.ax != s.fx || s.ay != s.fy)
}

func (s textSel) bounds() (cellPos, cellPos) {
	a := cellPos{X: s.ax, Y: s.ay}
	b := cellPos{X: s.fx, Y: s.fy}
	if a.Y > b.Y || (a.Y == b.Y && a.X > b.X) {
		return b, a
	}
	return a, b
}

func (m Model) withSelection(s string) string {
	if !m.altScreen || !m.sel.has() {
		return s
	}
	start, end := m.sel.bounds()
	return highlightSelection(s, start, end)
}

func (m Model) selectedText() string {
	if !m.sel.has() {
		return ""
	}
	start, end := m.sel.bounds()
	plain := m
	plain.sel = textSel{}
	return extractScreenText(plain.View(), start, end)
}

func extractScreenText(view string, start, end cellPos) string {
	if start.Y > end.Y || (start.Y == end.Y && start.X > end.X) {
		start, end = end, start
	}
	lines := strings.Split(view, "\n")
	var parts []string
	for y := start.Y; y <= end.Y; y++ {
		if y < 0 || y >= len(lines) {
			continue
		}
		plain := stripANSI(lines[y])
		x0, x1 := 0, runewidth.StringWidth(plain)-1
		if y == start.Y {
			x0 = start.X
		}
		if y == end.Y {
			x1 = end.X
		}
		if x1 < x0 || x1 < 0 {
			parts = append(parts, "")
			continue
		}
		parts = append(parts, sliceCells(plain, x0, x1))
	}
	return strings.Join(parts, "\n")
}

func sliceCells(plain string, startCol, endCol int) string {
	var b strings.Builder
	col := 0
	for _, r := range plain {
		w := runewidth.RuneWidth(r)
		if w < 0 {
			w = 0
		}
		if w == 0 {
			if col > startCol && col-1 <= endCol {
				b.WriteRune(r)
			}
			continue
		}
		if col+w-1 >= startCol && col <= endCol {
			b.WriteRune(r)
		}
		col += w
		if col > endCol {
			break
		}
	}
	return b.String()
}

func highlightSelection(view string, start, end cellPos) string {
	if start.Y > end.Y || (start.Y == end.Y && start.X > end.X) {
		start, end = end, start
	}
	lines := strings.Split(view, "\n")
	for y := range lines {
		if y < start.Y || y > end.Y {
			continue
		}
		x0 := 0
		x1 := 1 << 20
		if y == start.Y {
			x0 = start.X
		}
		if y == end.Y {
			x1 = end.X
		}
		lines[y] = highlightLine(lines[y], x0, x1)
	}
	return strings.Join(lines, "\n")
}

func highlightLine(line string, startCol, endCol int) string {
	var b strings.Builder
	col := 0
	i := 0
	on := false
	open := func() {
		if !on {
			b.WriteString("\x1b[7m")
			on = true
		}
	}
	closeInv := func() {
		if on {
			b.WriteString("\x1b[27m")
			on = false
		}
	}
	for i < len(line) {
		if line[i] == '\x1b' {
			n := skipESC(line, i)
			b.WriteString(line[i:n])
			i = n
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		w := runewidth.RuneWidth(r)
		if w < 0 {
			w = 0
		}
		if w == 0 {
			b.WriteString(line[i : i+size])
			i += size
			continue
		}
		if col <= endCol && col+w-1 >= startCol {
			open()
		} else {
			closeInv()
		}
		b.WriteString(line[i : i+size])
		col += w
		i += size
	}
	closeInv()
	return b.String()
}

func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			i = skipESC(s, i)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func skipESC(s string, i int) int {
	if i >= len(s) || s[i] != '\x1b' {
		return i
	}
	if i+1 >= len(s) {
		return i + 1
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
			j++
		}
		return len(s)
	case ']':
		j := i + 2
		for j < len(s) {
			if s[j] == '\x07' {
				return j + 1
			}
			if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
			j++
		}
		return len(s)
	case 'P', 'X', '^', '_':
		j := i + 2
		for j < len(s) {
			if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
			j++
		}
		return len(s)
	default:
		return i + 2
	}
}
