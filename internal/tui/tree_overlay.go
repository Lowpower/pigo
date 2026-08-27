package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/session"
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayTree
	overlayTreeLabel
	overlayTreeSummary
	overlayTreeCustom
	overlayFork
)

type treeOverlay struct {
	all           []flatNode
	vis           []flatNode
	cursor        int
	filter        string
	query         string
	folded        map[string]bool
	showLabelTime bool
	leafID        string
	path          map[string]bool
	labelBuf      string
	summaryIdx    int
	customBuf     string
	pendingID     string
	status        string
	multiRoots    bool
}

func (m Model) openTree() (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(s)})
		return m, nil
	}
	if m.engine == nil || m.engine.Opts.Session == nil {
		return note("no session")
	}
	sess := m.engine.Opts.Session
	roots := sess.GetTree()
	if len(roots) == 0 {
		return note("No entries in session")
	}
	leaf := sess.LeafID()
	path := map[string]bool{}
	for _, e := range sess.GetBranch("") {
		path[e.ID] = true
	}
	filter := m.cfg.TreeFilter()
	all := flattenForest(roots, leaf)
	tr := treeOverlay{
		all:        all,
		filter:     filter,
		folded:     map[string]bool{},
		leafID:     leaf,
		path:       path,
		multiRoots: len(roots) > 1,
	}
	tr.rebuild()
	tr.selectID(leaf)
	m.overlay = overlayTree
	m.tree = tr
	return m, nil
}

func (t *treeOverlay) rebuild() {
	t.vis = filterFlat(t.all, t.filter, t.query, t.leafID, t.folded)
	if t.cursor >= len(t.vis) {
		t.cursor = max(0, len(t.vis)-1)
	}
}

func (t *treeOverlay) selectID(id string) {
	if id == "" || len(t.vis) == 0 {
		return
	}
	for i, n := range t.vis {
		if n.node.Entry.ID == id {
			t.cursor = i
			return
		}
	}
	// walk parents
	cur := id
	for cur != "" {
		for i, n := range t.vis {
			if n.node.Entry.ID == cur {
				t.cursor = i
				return
			}
		}
		found := false
		for _, n := range t.all {
			if n.node.Entry.ID == cur && n.node.Entry.ParentID != nil {
				cur = *n.node.Entry.ParentID
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	t.cursor = len(t.vis) - 1
}

func (t *treeOverlay) current() (flatNode, bool) {
	if t.cursor < 0 || t.cursor >= len(t.vis) {
		return flatNode{}, false
	}
	return t.vis[t.cursor], true
}

func (t treeOverlay) hasKids(id string) bool {
	for _, n := range t.all {
		if n.node.Entry.ID == id {
			return len(n.node.Children) > 0
		}
	}
	return false
}

func (m Model) handleTreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.overlay {
	case overlayTreeLabel:
		switch key {
		case "enter":
			n, ok := m.tree.current()
			if ok && m.engine != nil && m.engine.Opts.Session != nil {
				_, _ = m.engine.Opts.Session.AppendLabel(n.node.Entry.ID, m.tree.labelBuf)
				m.tree.all = flattenForest(m.engine.Opts.Session.GetTree(), m.tree.leafID)
				m.tree.rebuild()
			}
			m.tree.labelBuf = ""
			m.overlay = overlayTree
			return m, nil
		case "esc", "escape":
			m.tree.labelBuf = ""
			m.overlay = overlayTree
			return m, nil
		case "backspace":
			if m.tree.labelBuf != "" {
				r := []rune(m.tree.labelBuf)
				m.tree.labelBuf = string(r[:len(r)-1])
			}
			return m, nil
		default:
			if isPrintableKey(key) {
				m.tree.labelBuf += key
			}
			return m, nil
		}
	case overlayTreeCustom:
		switch key {
		case "enter":
			return m.confirmTreeNav(true, m.tree.customBuf, false)
		case "esc", "escape":
			m.overlay = overlayTreeSummary
			return m, nil
		case "backspace":
			if m.tree.customBuf != "" {
				r := []rune(m.tree.customBuf)
				m.tree.customBuf = string(r[:len(r)-1])
			}
			return m, nil
		default:
			if isPrintableKey(key) {
				m.tree.customBuf += key
			}
			return m, nil
		}
	case overlayTreeSummary:
		switch key {
		case "up", "left":
			m.tree.summaryIdx = (m.tree.summaryIdx + 2) % 3
			return m, nil
		case "down", "right":
			m.tree.summaryIdx = (m.tree.summaryIdx + 1) % 3
			return m, nil
		case "enter":
			switch m.tree.summaryIdx {
			case 0:
				return m.confirmTreeNav(false, "", false)
			case 1:
				return m.confirmTreeNav(true, "", false)
			default:
				m.overlay = overlayTreeCustom
				m.tree.customBuf = ""
				return m, nil
			}
		case "esc", "escape":
			m.overlay = overlayTree
			m.tree.selectID(m.tree.pendingID)
			return m, nil
		}
		return m, nil
	}

	switch key {
	case "up":
		if len(m.tree.vis) > 0 {
			m.tree.cursor = (m.tree.cursor - 1 + len(m.tree.vis)) % len(m.tree.vis)
		}
	case "down":
		if len(m.tree.vis) > 0 {
			m.tree.cursor = (m.tree.cursor + 1) % len(m.tree.vis)
		}
	case "left", "pgup", "ctrl+b":
		m.tree.cursor = max(0, m.tree.cursor-max(5, m.height/2))
	case "right", "pgdown", "ctrl+f":
		if len(m.tree.vis) > 0 {
			m.tree.cursor = min(len(m.tree.vis)-1, m.tree.cursor+max(5, m.height/2))
		}
	case "ctrl+left", "alt+left":
		m.tree.foldOrUp()
	case "ctrl+right", "alt+right":
		m.tree.unfoldOrDown()
	case "ctrl+x":
		return m.copyTreeSelection()
	case "enter":
		n, ok := m.tree.current()
		if !ok {
			return m, nil
		}
		if n.node.Entry.ID == m.tree.leafID {
			m.tree.status = "Already at this point"
			return m, nil
		}
		m.tree.pendingID = n.node.Entry.ID
		if m.cfg.BranchSummarySkipPrompt() {
			return m.confirmTreeNav(false, "", false)
		}
		m.overlay = overlayTreeSummary
		m.tree.summaryIdx = 0
		return m, nil
	case "esc", "escape", "ctrl+c":
		if m.tree.query != "" {
			m.tree.query = ""
			m.tree.folded = map[string]bool{}
			m.tree.rebuild()
			return m, nil
		}
		m.overlay = overlayNone
		return m, nil
	case "ctrl+d":
		m.tree.filter = filterDefault
		m.tree.folded = map[string]bool{}
		m.tree.rebuild()
	case "ctrl+t":
		if m.tree.filter == filterNoTools {
			m.tree.filter = filterDefault
		} else {
			m.tree.filter = filterNoTools
		}
		m.tree.folded = map[string]bool{}
		m.tree.rebuild()
	case "ctrl+u":
		if m.tree.filter == filterUserOnly {
			m.tree.filter = filterDefault
		} else {
			m.tree.filter = filterUserOnly
		}
		m.tree.folded = map[string]bool{}
		m.tree.rebuild()
	case "ctrl+l":
		if m.tree.filter == filterLabeledOnly {
			m.tree.filter = filterDefault
		} else {
			m.tree.filter = filterLabeledOnly
		}
		m.tree.folded = map[string]bool{}
		m.tree.rebuild()
	case "ctrl+a":
		if m.tree.filter == filterAll {
			m.tree.filter = filterDefault
		} else {
			m.tree.filter = filterAll
		}
		m.tree.folded = map[string]bool{}
		m.tree.rebuild()
	case "ctrl+o":
		m.tree.filter = cycleFilter(m.tree.filter, false)
		m.tree.folded = map[string]bool{}
		m.tree.rebuild()
	case "shift+ctrl+o", "ctrl+shift+o":
		m.tree.filter = cycleFilter(m.tree.filter, true)
		m.tree.folded = map[string]bool{}
		m.tree.rebuild()
	case "shift+l":
		m.overlay = overlayTreeLabel
		n, ok := m.tree.current()
		if ok {
			m.tree.labelBuf = n.node.Label
		}
	case "shift+t":
		m.tree.showLabelTime = !m.tree.showLabelTime
	case "backspace":
		if m.tree.query != "" {
			r := []rune(m.tree.query)
			m.tree.query = string(r[:len(r)-1])
			m.tree.folded = map[string]bool{}
			m.tree.rebuild()
		}
	default:
		if isPrintableKey(key) {
			m.tree.query += key
			m.tree.folded = map[string]bool{}
			m.tree.rebuild()
		}
	}
	return m, nil
}

func (t *treeOverlay) foldOrUp() {
	n, ok := t.current()
	if !ok {
		return
	}
	parent, kids := visFamily(t.vis, t.all)
	id := n.node.Entry.ID
	if isFoldableID(id, parent, kids) && !t.folded[id] {
		t.folded[id] = true
		t.rebuild()
		t.selectID(id)
		return
	}
	t.cursor = findBranchSegmentStart(t.vis, t.cursor, "up", parent, kids)
}

func (t *treeOverlay) unfoldOrDown() {
	n, ok := t.current()
	if !ok {
		return
	}
	id := n.node.Entry.ID
	if t.folded[id] {
		delete(t.folded, id)
		t.rebuild()
		t.selectID(id)
		return
	}
	parent, kids := visFamily(t.vis, t.all)
	t.cursor = findBranchSegmentStart(t.vis, t.cursor, "down", parent, kids)
}

func (m Model) copyTreeSelection() (tea.Model, tea.Cmd) {
	n, ok := m.tree.current()
	if !ok {
		m.tree.status = "Selected entry has no text to copy"
		return m, nil
	}
	text := entryCopyText(n.node.Entry)
	if text == "" {
		m.tree.status = "Selected entry has no text to copy"
		return m, nil
	}
	m.clipOSC = osc52(text)
	m.tree.status = "Copied selected message to clipboard"
	return m, nil
}

func (m Model) confirmTreeNav(summarize bool, custom string, replace bool) (tea.Model, tea.Cmd) {
	target := m.tree.pendingID
	if m.turnBusy() {
		m.restoreQueuedToEditor()
		if m.cancel != nil {
			m.cancel()
		}
		m.pendingNav = &pendingNav{target: target, summarize: summarize, custom: custom, replace: replace}
		m.overlay = overlayNone
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("Stopping current response…")})
		return m, nil
	}
	return m.applyTreeNav(target, summarize, custom, replace)
}

func (m Model) applyTreeNav(target string, summarize bool, custom string, replace bool) (tea.Model, tea.Cmd) {
	if m.engine == nil || m.engine.Opts.Session == nil {
		m.overlay = overlayNone
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.summaryCancel = cancel
	res, err := m.engine.NavigateTree(ctx, target, session.NavigateOpts{
		Summarize:           summarize,
		CustomInstructions:  custom,
		ReplaceInstructions: replace,
	})
	cancel()
	m.summaryCancel = nil
	if err != nil {
		m.overlay = overlayNone
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(err.Error())})
		return m, nil
	}
	if res.Aborted {
		m.tree.pendingID = target
		m.overlay = overlayTree
		m.tree.selectID(target)
		return m, nil
	}
	if res.Cancelled {
		m.overlay = overlayNone
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("Navigation cancelled")})
		return m, nil
	}
	m.overlay = overlayNone
	m.reloadFromSession()
	if res.EditorText != "" && strings.TrimSpace(m.editor.Value()) == "" {
		m.editor.SetValue(res.EditorText)
	}
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("Navigated to selected point")})
	return m, nil
}

func (m *Model) reloadFromSession() {
	if m.engine == nil {
		return
	}
	m.history = m.engine.History()
	m.transcript = transcriptFromMessages(*m, m.history)
}

func transcriptFromMessages(m Model, msgs []ai.Message) []entry {
	out := make([]entry, 0, len(msgs))
	for _, msg := range msgs {
		switch msg.Role {
		case ai.RoleUser:
			out = append(out, entry{role: "user", rendered: m.userStyle.Render("› you") + "\n" + indent(msg.Text())})
		case ai.RoleAssistant:
			text := msg.Text()
			if text != "" {
				out = append(out, entry{role: "assistant", rendered: m.renderMarkdown(text)})
			}
		default:
			if msg.Text() != "" {
				out = append(out, entry{role: "tool", rendered: m.toolStyle.Render(firstLine(msg.Text()))})
			}
		}
	}
	return out
}

func (m Model) treeView() string {
	var b strings.Builder
	b.WriteString(m.titleStyle.Render("  Session Tree"))
	b.WriteString("\n")
	b.WriteString(m.metaStyle.Render("↑↓ enter  esc  ctrl+x copy  ctrl/alt+←→ branch  ctrl+d/t/u/l/a filter  shift+l label"))
	b.WriteString("\n")
	b.WriteString(m.metaStyle.Render("Type to search: " + m.tree.query))
	b.WriteString("\n")
	switch m.overlay {
	case overlayTreeLabel:
		b.WriteString("\n  Label: ")
		b.WriteString(m.tree.labelBuf)
		b.WriteString("█\n")
		return b.String()
	case overlayTreeSummary:
		opts := []string{"No summary", "Summarize", "Summarize with custom prompt"}
		b.WriteString("\n  Summarize branch?\n")
		for i, o := range opts {
			mark := "  "
			if i == m.tree.summaryIdx {
				mark = "› "
			}
			b.WriteString(mark + o + "\n")
		}
		return b.String()
	case overlayTreeCustom:
		b.WriteString("\n  Custom instructions: ")
		b.WriteString(m.tree.customBuf)
		b.WriteString("█\n")
		return b.String()
	}
	maxLines := m.height / 2
	if maxLines < 5 {
		maxLines = 5
	}
	start := 0
	if m.tree.cursor >= maxLines {
		start = m.tree.cursor - maxLines + 1
	}
	end := min(len(m.tree.vis), start+maxLines)
	tools := collectToolCalls(m.tree.all)
	var rows []treeViewRow
	for i := start; i < end; i++ {
		n := m.tree.vis[i]
		foldable := m.tree.hasKids(n.node.Entry.ID)
		rows = append(rows, buildTreeRow(n, i == m.tree.cursor, m.tree.path[n.node.Entry.ID], m.tree.showLabelTime, foldable, m.tree.folded[n.node.Entry.ID], m.tree.multiRoots, tools))
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	for _, line := range clipTreeRows(rows, width) {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if len(m.tree.vis) > 0 {
		b.WriteString(m.metaStyle.Render(fmtTreeStatus(m.tree)))
		b.WriteByte('\n')
	}
	if m.tree.status != "" {
		b.WriteString(m.metaStyle.Render(m.tree.status))
		b.WriteByte('\n')
	}
	return b.String()
}

func fmtTreeStatus(t treeOverlay) string {
	tag := ""
	if t.filter != filterDefault {
		tag = " [" + t.filter + "]"
	}
	return "  (" + strconv.Itoa(t.cursor+1) + "/" + strconv.Itoa(len(t.vis)) + ")" + tag
}

func (m Model) handleIdleEscape() (tea.Model, tea.Cmd) {
	if strings.TrimSpace(m.editor.Value()) != "" {
		return m, nil
	}
	act := m.cfg.DoubleEscape()
	if act == "none" {
		return m, nil
	}
	now := time.Now()
	if !m.lastEscape.IsZero() && now.Sub(m.lastEscape) < 500*time.Millisecond {
		m.lastEscape = time.Time{}
		if act == "fork" {
			return m.openFork()
		}
		return m.openTree()
	}
	m.lastEscape = now
	return m, nil
}
