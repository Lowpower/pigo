package tui

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/Lowpower/pigo/internal/session"
)

const (
	filterDefault     = "default"
	filterNoTools     = "no-tools"
	filterUserOnly    = "user-only"
	filterLabeledOnly = "labeled-only"
	filterAll         = "all"
)

var filterCycle = []string{filterDefault, filterNoTools, filterUserOnly, filterLabeledOnly, filterAll}

type gutterInfo struct {
	position int
	show     bool
}

type flatNode struct {
	node               session.TreeNode
	indent             int
	showConnector      bool
	isLast             bool
	gutters            []gutterInfo
	isVirtualRootChild bool
}

func flattenForest(roots []session.TreeNode, leafID string) []flatNode {
	contains := map[string]bool{}
	var mark func(n session.TreeNode) bool
	mark = func(n session.TreeNode) bool {
		has := leafID != "" && n.Entry.ID == leafID
		for _, c := range n.Children {
			if mark(c) {
				has = true
			}
		}
		contains[n.Entry.ID] = has
		return has
	}
	for _, r := range roots {
		mark(r)
	}

	ordered := append([]session.TreeNode(nil), roots...)
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if contains[ordered[j].Entry.ID] && !contains[ordered[i].Entry.ID] {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}

	type item struct {
		node               session.TreeNode
		indent             int
		justBranched       bool
		showConnector      bool
		isLast             bool
		gutters            []gutterInfo
		isVirtualRootChild bool
	}
	multi := len(ordered) > 1
	var stack []item
	for i := len(ordered) - 1; i >= 0; i-- {
		stack = append(stack, item{ordered[i], boolToIndent(multi), multi, multi, i == len(ordered)-1, nil, multi})
	}

	var out []flatNode
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, flatNode{it.node, it.indent, it.showConnector, it.isLast, it.gutters, it.isVirtualRootChild})

		kids := orderActiveFirst(it.node.Children, contains)
		multiKids := len(kids) > 1
		childIndent := it.indent
		if multiKids {
			childIndent = it.indent + 1
		} else if it.justBranched && it.indent > 0 {
			childIndent = it.indent + 1
		}
		displayIndent := it.indent
		if multi {
			if displayIndent > 0 {
				displayIndent--
			}
		}
		connectorPos := 0
		if displayIndent > 0 {
			connectorPos = displayIndent - 1
		}
		childGutters := it.gutters
		if it.showConnector && !it.isVirtualRootChild {
			childGutters = append(append([]gutterInfo(nil), it.gutters...), gutterInfo{position: connectorPos, show: !it.isLast})
		}
		for i := len(kids) - 1; i >= 0; i-- {
			stack = append(stack, item{kids[i], childIndent, multiKids, multiKids, i == len(kids)-1, childGutters, false})
		}
	}
	return out
}

func boolToIndent(multi bool) int {
	if multi {
		return 1
	}
	return 0
}

func orderActiveFirst(kids []session.TreeNode, contains map[string]bool) []session.TreeNode {
	var pri, rest []session.TreeNode
	for _, c := range kids {
		if contains[c.Entry.ID] {
			pri = append(pri, c)
		} else {
			rest = append(rest, c)
		}
	}
	return append(pri, rest...)
}

func nodePassesFilter(n flatNode, mode, leafID string) bool {
	e := n.node.Entry
	role := entryRoleOf(e)
	isLeaf := e.ID == leafID
	if role == "assistant" && !isLeaf && !assistantHasText(e) && !assistantErrorOrAbort(e) {
		return false
	}
	settings := e.Type == "label" || e.Type == "custom" || e.Type == "model_change" || e.Type == "thinking_level_change" || e.Type == "session_info"
	switch mode {
	case filterUserOnly:
		return (e.Type == "message" || e.Type == "") && role == "user"
	case filterNoTools:
		return !settings && role != "toolResult"
	case filterLabeledOnly:
		return n.node.Label != ""
	case filterAll:
		return true
	default:
		return !settings
	}
}

func filterFlat(nodes []flatNode, mode, query, leafID string, folded map[string]bool) []flatNode {
	tools := collectToolCalls(nodes)
	tokens := strings.Fields(strings.ToLower(query))
	var vis []flatNode
	for _, n := range nodes {
		if !nodePassesFilter(n, mode, leafID) {
			continue
		}
		if len(tokens) > 0 {
			text := strings.ToLower(searchableText(n.node, tools))
			ok := true
			for _, tok := range tokens {
				if !strings.Contains(text, tok) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		vis = append(vis, n)
	}
	if len(folded) > 0 {
		skip := map[string]bool{}
		for _, n := range nodes {
			pid := ""
			if n.node.Entry.ParentID != nil {
				pid = *n.node.Entry.ParentID
			}
			if pid != "" && (folded[pid] || skip[pid]) {
				skip[n.node.Entry.ID] = true
			}
		}
		var kept []flatNode
		for _, n := range vis {
			if !skip[n.node.Entry.ID] {
				kept = append(kept, n)
			}
		}
		vis = kept
	}
	return recalculateVisual(vis, nodes)
}

func recalculateVisual(filtered, all []flatNode) []flatNode {
	if len(filtered) == 0 {
		return filtered
	}
	visible := map[string]bool{}
	for _, n := range filtered {
		visible[n.node.Entry.ID] = true
	}
	byID := map[string]flatNode{}
	for _, n := range all {
		byID[n.node.Entry.ID] = n
	}
	parentOf := func(id string) string {
		n, ok := byID[id]
		if !ok || n.node.Entry.ParentID == nil {
			return ""
		}
		return *n.node.Entry.ParentID
	}
	nearest := func(id string) string {
		cur := parentOf(id)
		for cur != "" {
			if visible[cur] {
				return cur
			}
			cur = parentOf(cur)
		}
		return ""
	}
	children := map[string][]string{"": {}}
	for _, n := range filtered {
		anc := nearest(n.node.Entry.ID)
		children[anc] = append(children[anc], n.node.Entry.ID)
	}
	roots := children[""]
	multi := len(roots) > 1
	type item struct {
		id                 string
		indent             int
		justBranched       bool
		showConnector      bool
		isLast             bool
		gutters            []gutterInfo
		isVirtualRootChild bool
	}
	var stack []item
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, item{roots[i], boolToIndent(multi), multi, multi, i == len(roots)-1, nil, multi})
	}
	idx := map[string]int{}
	for i, n := range filtered {
		idx[n.node.Entry.ID] = i
	}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		i, ok := idx[it.id]
		if !ok {
			continue
		}
		filtered[i].indent = it.indent
		filtered[i].showConnector = it.showConnector
		filtered[i].isLast = it.isLast
		filtered[i].gutters = it.gutters
		filtered[i].isVirtualRootChild = it.isVirtualRootChild
		kids := children[it.id]
		multiKids := len(kids) > 1
		childIndent := it.indent
		if multiKids {
			childIndent = it.indent + 1
		} else if it.justBranched && it.indent > 0 {
			childIndent = it.indent + 1
		}
		displayIndent := it.indent
		if multi && displayIndent > 0 {
			displayIndent--
		}
		connectorPos := 0
		if displayIndent > 0 {
			connectorPos = displayIndent - 1
		}
		childGutters := it.gutters
		if it.showConnector && !it.isVirtualRootChild {
			childGutters = append(append([]gutterInfo(nil), it.gutters...), gutterInfo{position: connectorPos, show: !it.isLast})
		}
		for j := len(kids) - 1; j >= 0; j-- {
			stack = append(stack, item{kids[j], childIndent, multiKids, multiKids, j == len(kids)-1, childGutters, false})
		}
	}
	return filtered
}

const (
	treeGutterWidth              = 2
	minVisibleAnchorContentWidth = 4
	maxVisibleAnchorContentWidth = 20
	minAnchorContextWidth        = 2
	maxAnchorContextWidth        = 12
)

type toolCallInfo struct {
	name string
	args map[string]any
}

type treeViewRow struct {
	gutter    string
	body      string
	anchorCol int
	selected  bool
}

func renderTreeLine(n flatNode, selected, onPath, showLabelTime, foldable, folded bool, multiRoots bool) string {
	r := buildTreeRow(n, selected, onPath, showLabelTime, foldable, folded, multiRoots, nil, nil)
	return r.gutter + r.body
}

func buildTreeRow(n flatNode, selected, onPath, showLabelTime, foldable, folded bool, multiRoots bool, tools map[string]toolCallInfo, link pathLink) treeViewRow {
	gutter := "  "
	if selected {
		gutter = "› "
	}
	displayIndent := n.indent
	if multiRoots && displayIndent > 0 {
		displayIndent--
	}
	var prefix strings.Builder
	gutterAt := map[int]bool{}
	for _, g := range n.gutters {
		gutterAt[g.position] = g.show
	}
	for i := 0; i < displayIndent; i++ {
		if i == displayIndent-1 && n.showConnector && !n.isVirtualRootChild {
			if n.isLast {
				prefix.WriteString("└─")
			} else {
				prefix.WriteString("├─")
			}
			continue
		}
		if gutterAt[i] {
			prefix.WriteString("│ ")
		} else {
			prefix.WriteString("  ")
		}
	}
	foldMark := ""
	if foldable {
		if folded {
			foldMark = "⊞ "
		} else {
			foldMark = "⊟ "
		}
	}
	active := ""
	if onPath {
		active = "• "
	}
	label := ""
	if n.node.Label != "" {
		label = "[" + n.node.Label + "] "
		if showLabelTime && n.node.LabelTimestamp != "" {
			label += n.node.LabelTimestamp + " "
		}
	}
	lead := prefix.String() + foldMark + active + label
	return treeViewRow{
		gutter:    gutter,
		body:      lead + entryDisplay(n.node.Entry, tools, link),
		anchorCol: len([]rune(lead)),
		selected:  selected,
	}
}

func clipTreeRows(rows []treeViewRow, width int) []string {
	if width <= 0 {
		width = 80
	}
	viewportWidth := max(0, width-treeGutterWidth)
	maxBody := 0
	var selected *treeViewRow
	for i := range rows {
		w := len([]rune(rows[i].body))
		if w > maxBody {
			maxBody = w
		}
		if rows[i].selected {
			selected = &rows[i]
		}
	}
	maxScroll := max(0, maxBody-viewportWidth)
	scroll := 0
	if selected != nil && maxScroll > 0 {
		minVisible := min(maxVisibleAnchorContentWidth, max(minVisibleAnchorContentWidth, viewportWidth/3))
		if selected.anchorCol > viewportWidth-minVisible {
			ctx := min(maxAnchorContextWidth, max(minAnchorContextWidth, viewportWidth/4))
			scroll = min(maxScroll, selected.anchorCol-ctx)
		}
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		body := row.body
		if scroll > 0 {
			body = sliceByColumn(row.body, scroll, viewportWidth)
		}
		out[i] = truncateRunes(row.gutter+body, width)
	}
	return out
}

func sliceByColumn(s string, start, width int) string {
	r := []rune(s)
	if start < 0 {
		start = 0
	}
	if start >= len(r) || width <= 0 {
		return ""
	}
	end := min(len(r), start+width)
	return string(r[start:end])
}

func truncateRunes(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width])
}

func collectToolCalls(nodes []flatNode) map[string]toolCallInfo {
	out := map[string]toolCallInfo{}
	for _, n := range nodes {
		if entryRoleOf(n.node.Entry) != "assistant" {
			continue
		}
		var p struct {
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(n.node.Entry.Message, &p) != nil || len(p.Content) == 0 {
			continue
		}
		var blocks []struct {
			Type      string         `json:"type"`
			ID        string         `json:"id"`
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if json.Unmarshal(p.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type == "toolCall" && b.ID != "" {
				out[b.ID] = toolCallInfo{name: b.Name, args: b.Arguments}
			}
		}
	}
	return out
}

type pathLink func(display, raw string) string

func formatToolCall(name string, args map[string]any, link pathLink) string {
	if args == nil {
		args = map[string]any{}
	}
	if link == nil {
		link = func(display, _ string) string { return display }
	}
	shorten := func(p string) string {
		home, _ := os.UserHomeDir()
		if home != "" && strings.HasPrefix(p, home) {
			return "~" + p[len(home):]
		}
		return p
	}
	argStr := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k]; ok && v != nil {
				if s, ok := v.(string); ok {
					return s
				}
				b, _ := json.Marshal(v)
				return string(b)
			}
		}
		return ""
	}
	switch name {
	case "read":
		path := shorten(argStr("path", "file_path"))
		display := path
		_, hasOff := args["offset"]
		_, hasLim := args["limit"]
		if hasOff || hasLim {
			start := 1
			if hasOff {
				start = intFrom(args["offset"], 1)
			}
			if hasLim {
				end := start + intFrom(args["limit"], 0) - 1
				display += ":" + strconv.Itoa(start) + "-" + strconv.Itoa(end)
			} else {
				display += ":" + strconv.Itoa(start)
			}
		}
		return "[read: " + link(display, argStr("path", "file_path")) + "]"
	case "write":
		path := argStr("path", "file_path")
		return "[write: " + link(shorten(path), path) + "]"
	case "edit":
		path := argStr("path", "file_path")
		return "[edit: " + link(shorten(path), path) + "]"
	case "bash":
		raw := argStr("command")
		cmd := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(raw, "\n", " "), "\t", " "))
		if len([]rune(cmd)) > 50 {
			cmd = string([]rune(cmd)[:50]) + "..."
		}
		return "[bash: " + cmd + "]"
	case "grep":
		path := orDefault(argStr("path"), ".")
		return "[grep: /" + argStr("pattern") + "/ in " + link(shorten(path), path) + "]"
	case "find":
		path := orDefault(argStr("path"), ".")
		return "[find: " + argStr("pattern") + " in " + link(shorten(path), path) + "]"
	case "ls":
		path := orDefault(argStr("path"), ".")
		return "[ls: " + link(shorten(path), path) + "]"
	default:
		raw, _ := json.Marshal(args)
		s := string(raw)
		if len(s) > 40 {
			s = s[:40] + "..."
		}
		return "[" + name + ": " + s + "]"
	}
}

func intFrom(v any, fallback int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return fallback
	}
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func entryDisplay(e session.Entry, tools map[string]toolCallInfo, link pathLink) string {
	role := entryRoleOf(e)
	norm := func(s string) string {
		s = strings.ReplaceAll(s, "\n", " ")
		s = strings.ReplaceAll(s, "\t", " ")
		if len(s) > 200 {
			s = s[:200] + "…"
		}
		return strings.TrimSpace(s)
	}
	switch e.Type {
	case "branch_summary":
		return "[branch summary]: " + norm(e.Summary)
	case "compaction":
		return "[compaction]"
	case "label":
		if e.Label == nil {
			return "[label: (cleared)]"
		}
		return "[label: " + *e.Label + "]"
	case "session_info":
		if e.Name == "" {
			return "[title: empty]"
		}
		return "[title: " + e.Name + "]"
	case "custom_message":
		return "[custom]: " + norm(e.Summary)
	}
	switch role {
	case "user":
		return "user: " + norm(session.EntryContentText(e))
	case "assistant":
		t := session.EntryContentText(e)
		if t != "" {
			return "assistant: " + norm(t)
		}
		var p struct {
			StopReason   string `json:"stopReason"`
			ErrorMessage string `json:"errorMessage"`
		}
		_ = json.Unmarshal(e.Message, &p)
		if p.StopReason == "aborted" {
			return "assistant: (aborted)"
		}
		if p.ErrorMessage != "" {
			errMsg := norm(p.ErrorMessage)
			if len(errMsg) > 80 {
				errMsg = errMsg[:80]
			}
			return "assistant: " + errMsg
		}
		return "assistant: (no content)"
	case "toolResult":
		var p struct {
			ToolCallID string `json:"toolCallId"`
			ToolName   string `json:"toolName"`
		}
		_ = json.Unmarshal(e.Message, &p)
		if tools != nil {
			if tc, ok := tools[p.ToolCallID]; ok {
				return formatToolCall(tc.name, tc.args, link)
			}
		}
		if p.ToolName != "" {
			return "[" + p.ToolName + "]"
		}
		return "[tool]"
	case "bashExecution":
		var p struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(e.Message, &p)
		return "[bash]: " + norm(p.Command)
	default:
		if role != "" {
			return "[" + role + "]"
		}
		return "[" + e.Type + "]"
	}
}

func searchableText(n session.TreeNode, tools map[string]toolCallInfo) string {
	return n.Label + " " + entryDisplay(n.Entry, tools, nil) + " " + n.Entry.ID
}

func entryCopyText(e session.Entry) string {
	switch e.Type {
	case "custom_message":
		return strings.TrimSpace(session.EntryContentText(e))
	case "compaction", "branch_summary":
		return strings.TrimSpace(e.Summary)
	}
	role := entryRoleOf(e)
	switch role {
	case "bashExecution":
		var p struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(e.Message, &p)
		return strings.TrimSpace(p.Command)
	case "user", "assistant":
		text := strings.TrimSpace(session.EntryContentText(e))
		if text == "" && role == "assistant" {
			var p struct {
				ErrorMessage string `json:"errorMessage"`
			}
			_ = json.Unmarshal(e.Message, &p)
			text = strings.TrimSpace(p.ErrorMessage)
		}
		return text
	}
	return ""
}

func visFamily(vis, all []flatNode) (parent map[string]string, children map[string][]string) {
	visible := map[string]bool{}
	for _, n := range vis {
		visible[n.node.Entry.ID] = true
	}
	idParent := map[string]string{}
	for _, n := range all {
		pid := ""
		if n.node.Entry.ParentID != nil {
			pid = *n.node.Entry.ParentID
		}
		idParent[n.node.Entry.ID] = pid
	}
	parent = map[string]string{}
	children = map[string][]string{}
	for _, n := range vis {
		anc := ""
		cur := idParent[n.node.Entry.ID]
		for cur != "" {
			if visible[cur] {
				anc = cur
				break
			}
			cur = idParent[cur]
		}
		parent[n.node.Entry.ID] = anc
		children[anc] = append(children[anc], n.node.Entry.ID)
	}
	return parent, children
}

func isFoldableID(id string, parent map[string]string, children map[string][]string) bool {
	if len(children[id]) == 0 {
		return false
	}
	pid := parent[id]
	if pid == "" {
		return true
	}
	return len(children[pid]) > 1
}

func findBranchSegmentStart(vis []flatNode, cursor int, direction string, parent map[string]string, children map[string][]string) int {
	if cursor < 0 || cursor >= len(vis) {
		return cursor
	}
	indexBy := map[string]int{}
	for i, n := range vis {
		indexBy[n.node.Entry.ID] = i
	}
	currentID := vis[cursor].node.Entry.ID
	if direction == "down" {
		for {
			kids := children[currentID]
			if len(kids) == 0 {
				return indexBy[currentID]
			}
			if len(kids) > 1 {
				return indexBy[kids[0]]
			}
			currentID = kids[0]
		}
	}
	for {
		pid := parent[currentID]
		if pid == "" {
			return indexBy[currentID]
		}
		if len(children[pid]) > 1 {
			segment := indexBy[currentID]
			if segment < cursor {
				return segment
			}
		}
		currentID = pid
	}
}

func entryRoleOf(e session.Entry) string {
	var p struct {
		Role string `json:"role"`
	}
	_ = json.Unmarshal(e.Message, &p)
	return p.Role
}

func assistantHasText(e session.Entry) bool {
	return strings.TrimSpace(session.EntryContentText(e)) != ""
}

func assistantErrorOrAbort(e session.Entry) bool {
	var p struct {
		StopReason   string `json:"stopReason"`
		ErrorMessage string `json:"errorMessage"`
	}
	_ = json.Unmarshal(e.Message, &p)
	if p.ErrorMessage != "" {
		return true
	}
	return p.StopReason != "" && p.StopReason != "stop" && p.StopReason != "toolUse"
}

func cycleFilter(cur string, backward bool) string {
	idx := 0
	for i, m := range filterCycle {
		if m == cur {
			idx = i
			break
		}
	}
	if backward {
		idx = (idx - 1 + len(filterCycle)) % len(filterCycle)
	} else {
		idx = (idx + 1) % len(filterCycle)
	}
	return filterCycle[idx]
}

func isPrintableKey(s string) bool {
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "ctrl+") || strings.HasPrefix(s, "alt+") || strings.HasPrefix(s, "shift+") {
		if s == "shift+l" || s == "shift+t" {
			return false
		}
	}
	r := []rune(s)
	return len(r) == 1 && unicode.IsPrint(r[0]) && !unicode.IsControl(r[0])
}
