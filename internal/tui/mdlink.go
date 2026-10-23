package tui

import "strings"

const linkPlaceholderBase = rune(0xE000)

type mdLink struct {
	start int
	end   int
	label string
	dest  string
}

func placeholderMarkdownLinks(s string) (string, map[rune]string) {
	spans := markdownLinks(s)
	if len(spans) == 0 {
		return s, nil
	}
	subs := make(map[rune]string, len(spans))
	used := map[rune]bool{}
	for _, r := range s {
		if r >= linkPlaceholderBase && r < linkPlaceholderBase+256 {
			used[r] = true
		}
	}
	next := linkPlaceholderBase
	for i := len(spans) - 1; i >= 0; i-- {
		sp := spans[i]
		r := next
		for used[r] {
			r++
		}
		if r >= linkPlaceholderBase+256 {
			break
		}
		used[r] = true
		next = r + 1
		subs[r] = osc8(sp.label, sp.dest)
		s = s[:sp.start] + string(r) + s[sp.end:]
	}
	return s, subs
}

func restoreMarkdownLinks(s string, subs map[rune]string) string {
	if len(subs) == 0 || s == "" {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if repl, ok := subs[r]; ok {
			b.WriteString(repl)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func markdownLinks(s string) []mdLink {
	mask := codeMask(s)
	var out []mdLink
	for i := 0; i < len(s); {
		if mask[i] {
			i++
			continue
		}
		if s[i] == '[' && !escaped(s, i) && !imageMark(s, i) {
			if end, label, dest, ok := parseInlineLink(s, mask, i); ok {
				out = append(out, mdLink{start: i, end: end, label: label, dest: dest})
				i = end
				continue
			}
		}
		if s[i] == '<' && !escaped(s, i) {
			if end, label, dest, ok := parseAngleLink(s, i); ok {
				out = append(out, mdLink{start: i, end: end, label: label, dest: dest})
				i = end
				continue
			}
		}
		i++
	}
	return out
}

func imageMark(s string, i int) bool {
	return i > 0 && s[i-1] == '!' && !escaped(s, i-1)
}

func escaped(s string, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

func codeMask(s string) []bool {
	mask := make([]bool, len(s))
	off := 0
	inFence := false
	var fenceByte byte
	fenceLen := 0
	for _, line := range strings.SplitAfter(s, "\n") {
		body := strings.TrimRight(line, "\n")
		if inFence {
			for k := 0; k < len(line); k++ {
				mask[off+k] = true
			}
			if fenceCloser(body, fenceByte, fenceLen) {
				inFence = false
			}
		} else if marker, n, ok := openingFence(body); ok {
			inFence = true
			fenceByte = marker
			fenceLen = n
			for k := 0; k < len(line); k++ {
				mask[off+k] = true
			}
		} else {
			maskCodeSpans(mask[off:off+len(body)], body)
		}
		off += len(line)
	}
	return mask
}

func openingFence(line string) (marker byte, n int, ok bool) {
	trim := strings.TrimLeft(line, " \t")
	if len(trim) < 3 {
		return 0, 0, false
	}
	marker = trim[0]
	if marker != '`' && marker != '~' {
		return 0, 0, false
	}
	for n < len(trim) && trim[n] == marker {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	if marker == '`' && strings.ContainsRune(trim[n:], '`') {
		return 0, 0, false
	}
	return marker, n, true
}

func fenceCloser(line string, marker byte, minLen int) bool {
	trim := strings.TrimLeft(line, " \t")
	n := 0
	for n < len(trim) && trim[n] == marker {
		n++
	}
	if n < minLen {
		return false
	}
	return strings.TrimSpace(trim[n:]) == ""
}

func maskCodeSpans(mask []bool, line string) {
	i := 0
	for i < len(line) {
		if line[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(line) && line[i+n] == '`' {
			n++
		}
		rest := line[i+n:]
		k := 0
		closer := -1
		for k < len(rest) {
			rel := strings.IndexByte(rest[k:], '`')
			if rel < 0 {
				break
			}
			at := k + rel
			end := at
			for end < len(rest) && rest[end] == '`' {
				end++
			}
			if end-at == n {
				closer = end
				break
			}
			k = end
		}
		if closer < 0 {
			i += n
			continue
		}
		stop := i + n + closer
		for j := i; j < stop && j < len(mask); j++ {
			mask[j] = true
		}
		i = stop
	}
}

func parseInlineLink(s string, mask []bool, i int) (end int, label, dest string, ok bool) {
	j := i + 1
	depth := 1
	for j < len(s) && depth > 0 {
		if mask[j] {
			j++
			continue
		}
		if s[j] == '\\' && j+1 < len(s) {
			j += 2
			continue
		}
		switch s[j] {
		case '[':
			depth++
		case ']':
			depth--
		case '\n':
			if j+1 < len(s) && s[j+1] == '\n' {
				return 0, "", "", false
			}
		}
		if depth > 0 {
			j++
		}
	}
	if depth != 0 || j >= len(s) || s[j] != ']' || j+1 >= len(s) || s[j+1] != '(' {
		return 0, "", "", false
	}
	rawLabel := s[i+1 : j]
	dest, end, ok = parseLinkDest(s, j+2)
	if !ok || dest == "" {
		return 0, "", "", false
	}
	return end, unescapeMD(rawLabel), dest, true
}

func parseLinkDest(s string, i int) (dest string, end int, ok bool) {
	i = skipLinkSpace(s, i)
	if i >= len(s) {
		return "", 0, false
	}
	if s[i] == '<' {
		k := strings.IndexByte(s[i+1:], '>')
		if k < 0 {
			return "", 0, false
		}
		dest = unescapeMD(s[i+1 : i+1+k])
		i += k + 2
	} else {
		start := i
		depth := 0
		for i < len(s) {
			if s[i] == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if s[i] == '(' {
				depth++
				i++
				continue
			}
			if s[i] == ')' && depth > 0 {
				depth--
				i++
				continue
			}
			if depth == 0 && (s[i] == ')' || s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
				break
			}
			i++
		}
		if i == start {
			return "", 0, false
		}
		dest = unescapeMD(s[start:i])
	}
	k := skipLinkSpace(s, i)
	if k < len(s) && (s[k] == '"' || s[k] == '\'' || s[k] == '(') {
		closer := s[k]
		if closer == '(' {
			closer = ')'
		}
		rel := strings.IndexByte(s[k+1:], closer)
		if rel < 0 {
			return "", 0, false
		}
		k += rel + 2
		k = skipLinkSpace(s, k)
	}
	if k >= len(s) || s[k] != ')' {
		return "", 0, false
	}
	return dest, k + 1, true
}

func skipLinkSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	return i
}

func parseAngleLink(s string, i int) (end int, label, dest string, ok bool) {
	k := strings.IndexByte(s[i+1:], '>')
	if k <= 0 {
		return 0, "", "", false
	}
	body := s[i+1 : i+1+k]
	if strings.ContainsAny(body, " \t\n<>") {
		return 0, "", "", false
	}
	end = i + 1 + k + 1
	if strings.HasPrefix(body, "https://") || strings.HasPrefix(body, "http://") {
		return end, body, body, true
	}
	at := strings.IndexByte(body, '@')
	if at > 0 && at < len(body)-1 && strings.Contains(body[at+1:], ".") && !strings.Contains(body, ":") {
		return end, body, "mailto:" + body, true
	}
	return 0, "", "", false
}

func unescapeMD(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
