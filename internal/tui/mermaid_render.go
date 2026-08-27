package tui

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

type flowNode struct {
	id, label string
}

type flowEdge struct {
	from, to string
}

var classRe = regexp.MustCompile(`:::[\w-]+`)

func renderMermaidArt(src string) ([]string, int, bool) {
	dir, nodes, edges, ok := parseFlowchart(src)
	if !ok || len(nodes) == 0 {
		return nil, 0, false
	}
	boxes := map[string]boxArt{}
	for _, n := range nodes {
		boxes[n.id] = makeBox(n.label)
	}
	cols := layerNodes(nodes, edges)
	if dir == "RL" {
		reverseLayers(cols)
	}
	if dir == "BT" {
		reverseLayers(cols)
	}
	horizontal := dir == "LR" || dir == "RL"
	if horizontal {
		return drawLR(cols, boxes, edges)
	}
	return drawTD(cols, boxes, edges)
}

func parseFlowchart(src string) (dir string, nodes []flowNode, edges []flowEdge, ok bool) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var lines []string
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "%%"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		if low == "end" || strings.HasPrefix(low, "subgraph") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "", nil, nil, false
	}
	header := strings.Fields(lines[0])
	if len(header) == 0 {
		return "", nil, nil, false
	}
	kind := strings.ToLower(header[0])
	if kind != "flowchart" && kind != "graph" {
		return "", nil, nil, false
	}
	dir = "TD"
	if len(header) > 1 {
		switch strings.ToUpper(header[1]) {
		case "LR", "RL":
			dir = strings.ToUpper(header[1])
		case "TD", "TB":
			dir = "TD"
		case "BT":
			dir = "BT"
		}
	}
	body := classRe.ReplaceAllString(strings.Join(lines[1:], "\n"), "")
	nodes, edges = parseFlowBody(body)
	if len(nodes) == 0 {
		return "", nil, nil, false
	}
	return dir, nodes, edges, true
}

func parseFlowBody(s string) ([]flowNode, []flowEdge) {
	s = strings.ReplaceAll(s, "\n", " ")
	order := []string{}
	labels := map[string]string{}
	seen := map[string]bool{}
	var edges []flowEdge
	add := func(id, label string) {
		if id == "" {
			return
		}
		if label == "" {
			label = id
		}
		if !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
		if _, ok := labels[id]; !ok || label != id {
			labels[id] = label
		}
	}
	i := 0
	prev := ""
	pendingEdge := false
	for i < len(s) {
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}
		if pendingEdge {
			if n := skipEdgeLabel(s[i:]); n > 0 {
				i += n
				continue
			}
		}
		if op, n := matchEdgeOp(s[i:]); n > 0 {
			if prev != "" {
				pendingEdge = true
			}
			i += n
			_ = op
			continue
		}
		id, label, n := matchNode(s[i:])
		if n == 0 {
			i++
			continue
		}
		add(id, label)
		if pendingEdge && prev != "" {
			edges = append(edges, flowEdge{from: prev, to: id})
			pendingEdge = false
		}
		prev = id
		i += n
	}
	nodes := make([]flowNode, 0, len(order))
	for _, id := range order {
		nodes = append(nodes, flowNode{id: id, label: labels[id]})
	}
	return nodes, edges
}

func skipEdgeLabel(s string) int {
	if !strings.HasPrefix(s, "|") {
		return 0
	}
	j := 1
	for j < len(s) && s[j] != '|' {
		j++
	}
	if j < len(s) && s[j] == '|' {
		return j + 1
	}
	return 0
}

func matchEdgeOp(s string) (string, int) {
	i := 0
	if strings.HasPrefix(s, "<") {
		i++
	}
	rest := s[i:]
	switch {
	case strings.HasPrefix(rest, "-.->"):
		return "-.->", i + 4
	case strings.HasPrefix(rest, "-->"):
		return "-->", i + 3
	case strings.HasPrefix(rest, "==>"):
		return "==>", i + 3
	case strings.HasPrefix(rest, "---"):
		return "---", i + 3
	case strings.HasPrefix(rest, "==="):
		return "===", i + 3
	case strings.HasPrefix(rest, "--"):
		return "--", i + 2
	default:
		return "", 0
	}
}

func matchNode(s string) (id, label string, n int) {
	if s == "" {
		return "", "", 0
	}
	r, _ := utf8.DecodeRuneInString(s)
	if !unicode.IsLetter(r) && r != '_' {
		return "", "", 0
	}
	i := 0
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			i += w
			continue
		}
		break
	}
	id = s[:i]
	j := i
	if j < len(s) {
		switch s[j] {
		case '[':
			if k := strings.IndexByte(s[j:], ']'); k > 0 {
				label = strings.TrimSpace(s[j+1 : j+k])
				j += k + 1
			}
		case '{':
			if k := strings.IndexByte(s[j:], '}'); k > 0 {
				label = strings.TrimSpace(s[j+1 : j+k])
				j += k + 1
			}
		case '(':
			if strings.HasPrefix(s[j:], "((") {
				if k := strings.Index(s[j+2:], "))"); k >= 0 {
					label = strings.TrimSpace(s[j+2 : j+2+k])
					j += 2 + k + 2
					break
				}
			}
			if k := strings.IndexByte(s[j:], ')'); k > 0 {
				label = strings.TrimSpace(s[j+1 : j+k])
				j += k + 1
			}
		}
	}
	if label == "" {
		label = id
	}
	return id, label, j
}

func layerNodes(nodes []flowNode, edges []flowEdge) [][]flowNode {
	byID := map[string]flowNode{}
	incoming := map[string]int{}
	adj := map[string][]string{}
	for _, n := range nodes {
		byID[n.id] = n
		incoming[n.id] = 0
	}
	for _, e := range edges {
		if _, ok := byID[e.from]; !ok {
			continue
		}
		if _, ok := byID[e.to]; !ok {
			continue
		}
		adj[e.from] = append(adj[e.from], e.to)
		incoming[e.to]++
	}
	layer := map[string]int{}
	var q []string
	for _, n := range nodes {
		if incoming[n.id] == 0 {
			q = append(q, n.id)
			layer[n.id] = 0
		}
	}
	if len(q) == 0 && len(nodes) > 0 {
		q = append(q, nodes[0].id)
		layer[nodes[0].id] = 0
	}
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		for _, v := range adj[u] {
			if _, ok := layer[v]; ok {
				continue
			}
			layer[v] = layer[u] + 1
			q = append(q, v)
		}
	}
	maxL := 0
	for _, n := range nodes {
		if _, ok := layer[n.id]; !ok {
			layer[n.id] = 0
		}
		if layer[n.id] > maxL {
			maxL = layer[n.id]
		}
	}
	out := make([][]flowNode, maxL+1)
	for _, n := range nodes {
		out[layer[n.id]] = append(out[layer[n.id]], n)
	}
	return out
}

func reverseLayers(cols [][]flowNode) {
	for i, j := 0, len(cols)-1; i < j; i, j = i+1, j-1 {
		cols[i], cols[j] = cols[j], cols[i]
	}
}

type boxArt struct {
	lines []string
	w, h  int
}

func makeBox(label string) boxArt {
	if label == "" {
		label = " "
	}
	inner := runewidth.StringWidth(label) + 2
	if inner < 1 {
		inner = 1
	}
	pad := inner - runewidth.StringWidth(label)
	left := pad / 2
	right := pad - left
	mid := "│" + strings.Repeat(" ", left) + label + strings.Repeat(" ", right) + "│"
	bar := strings.Repeat("─", inner)
	lines := []string{"┌" + bar + "┐", mid, "└" + bar + "┘"}
	return boxArt{lines: lines, w: runewidth.StringWidth(lines[0]), h: 3}
}

type grid struct {
	rows [][]rune
}

func (g *grid) set(r, c int, ch rune) {
	for len(g.rows) <= r {
		g.rows = append(g.rows, nil)
	}
	for len(g.rows[r]) <= c {
		g.rows[r] = append(g.rows[r], ' ')
	}
	g.rows[r][c] = ch
}

func (g *grid) put(r, c int, s string) {
	x := c
	for _, ch := range s {
		w := runewidth.RuneWidth(ch)
		if w <= 0 {
			w = 1
		}
		g.set(r, x, ch)
		for k := 1; k < w; k++ {
			g.set(r, x+k, 0)
		}
		x += w
	}
}

func (g *grid) lines() ([]string, int) {
	maxW := 0
	out := make([]string, len(g.rows))
	for i, row := range g.rows {
		var b strings.Builder
		for _, ch := range row {
			if ch == 0 {
				continue
			}
			b.WriteRune(ch)
		}
		s := strings.TrimRight(b.String(), " ")
		out[i] = s
		if w := runewidth.StringWidth(s); w > maxW {
			maxW = w
		}
	}
	return out, maxW
}

type boxPos struct {
	r, c, w, h int
}

func drawLR(cols [][]flowNode, boxes map[string]boxArt, edges []flowEdge) ([]string, int, bool) {
	pos := map[string]boxPos{}
	x := 0
	const gap = 4
	for _, col := range cols {
		colW := 0
		y := 0
		for _, n := range col {
			b := boxes[n.id]
			pos[n.id] = boxPos{r: y, c: x, w: b.w, h: b.h}
			if b.w > colW {
				colW = b.w
			}
			y += b.h + 1
		}
		x += colW + gap
	}
	var g grid
	for id, p := range pos {
		b := boxes[id]
		for i, line := range b.lines {
			g.put(p.r+i, p.c, line)
		}
	}
	for _, e := range edges {
		a, ok1 := pos[e.from]
		b, ok2 := pos[e.to]
		if !ok1 || !ok2 {
			continue
		}
		if a.c+a.w-1 >= b.c {
			continue
		}
		row := a.r + 1
		if b.r+1 < row {
			row = b.r + 1
		}
		g.set(row, a.c+a.w-1, '├')
		for x := a.c + a.w; x < b.c-1; x++ {
			g.set(row, x, '─')
		}
		g.set(row, b.c-1, '▶')
	}
	lines, w := g.lines()
	if w == 0 {
		return nil, 0, false
	}
	return lines, w, true
}

func drawTD(cols [][]flowNode, boxes map[string]boxArt, edges []flowEdge) ([]string, int, bool) {
	pos := map[string]boxPos{}
	y := 0
	const vgap = 2
	for _, row := range cols {
		x := 0
		rowH := 3
		for _, n := range row {
			b := boxes[n.id]
			pos[n.id] = boxPos{r: y, c: x, w: b.w, h: b.h}
			x += b.w + 2
			if b.h > rowH {
				rowH = b.h
			}
		}
		y += rowH + vgap
	}
	var g grid
	for id, p := range pos {
		b := boxes[id]
		for i, line := range b.lines {
			g.put(p.r+i, p.c, line)
		}
	}
	for _, e := range edges {
		a, ok1 := pos[e.from]
		b, ok2 := pos[e.to]
		if !ok1 || !ok2 || a.r+a.h-1 >= b.r {
			continue
		}
		cx := a.c + a.w/2
		g.set(a.r+a.h-1, cx, '┬')
		for y := a.r + a.h; y < b.r-1; y++ {
			g.set(y, cx, '│')
		}
		g.set(b.r-1, cx, '▼')
	}
	lines, w := g.lines()
	if w == 0 {
		return nil, 0, false
	}
	return lines, w, true
}
