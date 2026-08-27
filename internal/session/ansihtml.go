package session

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

var ansiColors = []string{
	"#000000", "#800000", "#008000", "#808000", "#000080", "#800080", "#008080", "#c0c0c0",
	"#808080", "#ff0000", "#00ff00", "#ffff00", "#0000ff", "#ff00ff", "#00ffff", "#ffffff",
}

var ansiSGRRe = regexp.MustCompile(`\x1b\[([\d;]*)m`)

type textStyle struct {
	fg, bg                       string
	bold, dim, italic, underline bool
}

func (s textStyle) css() string {
	var parts []string
	if s.fg != "" {
		parts = append(parts, "color:"+s.fg)
	}
	if s.bg != "" {
		parts = append(parts, "background-color:"+s.bg)
	}
	if s.bold {
		parts = append(parts, "font-weight:bold")
	}
	if s.dim {
		parts = append(parts, "opacity:0.6")
	}
	if s.italic {
		parts = append(parts, "font-style:italic")
	}
	if s.underline {
		parts = append(parts, "text-decoration:underline")
	}
	return strings.Join(parts, ";")
}

func (s textStyle) styled() bool {
	return s.fg != "" || s.bg != "" || s.bold || s.dim || s.italic || s.underline
}

func applySGR(params []int, style *textStyle) {
	for i := 0; i < len(params); i++ {
		code := params[i]
		switch {
		case code == 0:
			*style = textStyle{}
		case code == 1:
			style.bold = true
		case code == 2:
			style.dim = true
		case code == 3:
			style.italic = true
		case code == 4:
			style.underline = true
		case code == 22:
			style.bold = false
			style.dim = false
		case code == 23:
			style.italic = false
		case code == 24:
			style.underline = false
		case code >= 30 && code <= 37:
			style.fg = ansiColors[code-30]
		case code == 38:
			if i+2 < len(params) && params[i+1] == 5 {
				style.fg = color256ToHex(params[i+2])
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 {
				style.fg = fmt.Sprintf("rgb(%d,%d,%d)", params[i+2], params[i+3], params[i+4])
				i += 4
			}
		case code == 39:
			style.fg = ""
		case code >= 40 && code <= 47:
			style.bg = ansiColors[code-40]
		case code == 48:
			if i+2 < len(params) && params[i+1] == 5 {
				style.bg = color256ToHex(params[i+2])
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 {
				style.bg = fmt.Sprintf("rgb(%d,%d,%d)", params[i+2], params[i+3], params[i+4])
				i += 4
			}
		case code == 49:
			style.bg = ""
		case code >= 90 && code <= 97:
			style.fg = ansiColors[code-90+8]
		case code >= 100 && code <= 107:
			style.bg = ansiColors[code-100+8]
		}
	}
}

func color256ToHex(index int) string {
	if index < 0 {
		index = 0
	}
	if index > 255 {
		index = 255
	}
	if index < 16 {
		return ansiColors[index]
	}
	if index < 232 {
		cube := index - 16
		r := cube / 36
		g := (cube % 36) / 6
		b := cube % 6
		to := func(n int) int {
			if n == 0 {
				return 0
			}
			return 55 + n*40
		}
		return fmt.Sprintf("#%02x%02x%02x", to(r), to(g), to(b))
	}
	gray := 8 + (index-232)*10
	return fmt.Sprintf("#%02x%02x%02x", gray, gray, gray)
}

func ansiToHTML(text string) string {
	style := textStyle{}
	var b strings.Builder
	last := 0
	inSpan := false
	for _, match := range ansiSGRRe.FindAllStringSubmatchIndex(text, -1) {
		if match[0] > last {
			b.WriteString(html.EscapeString(text[last:match[0]]))
		}
		if inSpan {
			b.WriteString("</span>")
			inSpan = false
		}
		paramStr := text[match[2]:match[3]]
		var params []int
		if paramStr == "" {
			params = []int{0}
		} else {
			for _, p := range strings.Split(paramStr, ";") {
				n, _ := strconv.Atoi(p)
				params = append(params, n)
			}
		}
		applySGR(params, &style)
		if style.styled() {
			b.WriteString(`<span style="`)
			b.WriteString(style.css())
			b.WriteString(`">`)
			inSpan = true
		}
		last = match[1]
	}
	if last < len(text) {
		b.WriteString(html.EscapeString(text[last:]))
	}
	if inSpan {
		b.WriteString("</span>")
	}
	return b.String()
}

func ansiLinesToHTML(lines []string) string {
	parts := make([]string, len(lines))
	for i, line := range lines {
		inner := ansiToHTML(line)
		if inner == "" {
			inner = "&nbsp;"
		}
		parts[i] = `<div class="ansi-line">` + inner + `</div>`
	}
	return strings.Join(parts, "")
}
