package tui

import (
	"strings"
	"unicode"
)

type mermaidOpts struct {
	mode      string
	width     int
	streaming bool
	thinking  bool
}

func transformMermaid(md string, opt mermaidOpts) string {
	mode := opt.mode
	if mode == "" {
		mode = "streaming"
	}
	if mode == "off" || opt.thinking || (opt.streaming && mode != "streaming") {
		return md
	}
	return replaceMermaidFences(md, opt.width, opt.streaming)
}

func replaceMermaidFences(md string, width int, streaming bool) string {
	var b strings.Builder
	i := 0
	for i < len(md) {
		start, openEnd, lang := findFenceOpen(md, i)
		if start < 0 {
			b.WriteString(md[i:])
			break
		}
		b.WriteString(md[i:start])
		bodyStart := openEnd
		if bodyStart < len(md) && md[bodyStart] == '\n' {
			bodyStart++
		}
		closeStart, closeEnd := findFenceClose(md, bodyStart)
		var body, raw string
		if closeStart < 0 {
			body = md[bodyStart:]
			raw = md[start:]
			i = len(md)
		} else {
			body = md[bodyStart:closeStart]
			raw = md[start:closeEnd]
			i = closeEnd
		}
		if !isMermaidLang(lang) {
			b.WriteString(raw)
			continue
		}
		lines, artW, ok := renderMermaidArt(body)
		if !ok || (width > 0 && artW > width) {
			b.WriteString(raw)
			continue
		}
		out := strings.Join(lines, "\n")
		if !streaming {
			out = "```\n" + out + "\n```"
		}
		if strings.HasSuffix(raw, "\n") && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		b.WriteString(out)
	}
	return b.String()
}

func isMermaidLang(lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexAny(lang, " \t"); i >= 0 {
		lang = lang[:i]
	}
	return lang == "mermaid"
}

func findFenceOpen(s string, from int) (start, openEnd int, lang string) {
	pos := from
	for pos <= len(s) {
		if pos > 0 && pos < len(s) && s[pos-1] != '\n' {
			nl := strings.IndexByte(s[pos:], '\n')
			if nl < 0 {
				return -1, 0, ""
			}
			pos += nl + 1
			continue
		}
		if pos == len(s) {
			return -1, 0, ""
		}
		j := pos
		spaces := 0
		for j < len(s) && s[j] == ' ' && spaces < 3 {
			j++
			spaces++
		}
		if j+3 <= len(s) && s[j:j+3] == "```" {
			k := j + 3
			for k < len(s) && s[k] == '`' {
				k++
			}
			langStart := k
			for k < len(s) && s[k] != '\n' && !unicode.IsSpace(rune(s[k])) {
				k++
			}
			lang := strings.TrimSpace(s[langStart:k])
			openEnd := k
			for openEnd < len(s) && s[openEnd] != '\n' {
				openEnd++
			}
			return pos, openEnd, lang
		}
		nl := strings.IndexByte(s[pos:], '\n')
		if nl < 0 {
			return -1, 0, ""
		}
		pos += nl + 1
	}
	return -1, 0, ""
}

func findFenceClose(s string, from int) (closeStart, closeEnd int) {
	pos := from
	for pos <= len(s) {
		lineStart := pos
		if pos > 0 && pos < len(s) && s[pos-1] != '\n' {
			nl := strings.IndexByte(s[pos:], '\n')
			if nl < 0 {
				return -1, 0
			}
			pos += nl + 1
			continue
		}
		if pos == len(s) {
			return -1, 0
		}
		j := pos
		spaces := 0
		for j < len(s) && s[j] == ' ' && spaces < 3 {
			j++
			spaces++
		}
		if j+3 <= len(s) && s[j:j+3] == "```" {
			k := j + 3
			for k < len(s) && s[k] == '`' {
				k++
			}
			for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
				k++
			}
			if k == len(s) || s[k] == '\n' {
				if k < len(s) && s[k] == '\n' {
					k++
				}
				return lineStart, k
			}
		}
		nl := strings.IndexByte(s[pos:], '\n')
		if nl < 0 {
			return -1, 0
		}
		pos += nl + 1
	}
	return -1, 0
}
