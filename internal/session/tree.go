package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TreeNode is one node of the session parentId tree.
type TreeNode struct {
	Entry          Entry      `json:"entry"`
	Children       []TreeNode `json:"children"`
	Label          string     `json:"label,omitempty"`
	LabelTimestamp string     `json:"labelTimestamp,omitempty"`
}

// Summary is a one-line listing used by /resume.
type Summary struct {
	Path         string
	ID           string
	Name         string
	Cwd          string
	FirstMessage string
	Modified     time.Time
}

// Branch moves the leaf pointer so the next append is a child of id.
func (m *Manager) Branch(id string) error {
	if id == "" {
		m.leafID = ""
		return nil
	}
	for _, e := range m.entries {
		if e != nil && e.ID == id {
			m.leafID = id
			return nil
		}
	}
	return fmt.Errorf("entry %s not found", id)
}

// GetBranch walks from fromID (or the current leaf) to the root, oldest first.
func (m *Manager) GetBranch(fromID string) []Entry {
	byID := map[string]*Entry{}
	for _, e := range m.entries {
		if e != nil {
			byID[e.ID] = e
		}
	}
	start := fromID
	if start == "" {
		start = m.leafID
	}
	var rev []Entry
	cur := byID[start]
	seen := map[string]bool{}
	for cur != nil && !seen[cur.ID] {
		seen[cur.ID] = true
		rev = append(rev, *cur)
		if cur.ParentID == nil || *cur.ParentID == "" || *cur.ParentID == cur.ID {
			break
		}
		cur = byID[*cur.ParentID]
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// GetTree returns the parentId forest.
func (m *Manager) GetTree() []TreeNode {
	return buildTree(m.Entries())
}

func buildTree(entries []Entry) []TreeNode {
	byID := map[string]Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	labels, labelTS := resolvedLabels(entries)
	children := map[string][]string{}
	var rootIDs []string
	for _, e := range entries {
		if e.ParentID == nil || *e.ParentID == "" || *e.ParentID == e.ID {
			rootIDs = append(rootIDs, e.ID)
			continue
		}
		if _, ok := byID[*e.ParentID]; !ok {
			rootIDs = append(rootIDs, e.ID)
			continue
		}
		children[*e.ParentID] = append(children[*e.ParentID], e.ID)
	}
	var build func(id string) TreeNode
	build = func(id string) TreeNode {
		n := TreeNode{Entry: byID[id], Children: []TreeNode{}, Label: labels[id], LabelTimestamp: labelTS[id]}
		kids := append([]string(nil), children[id]...)
		for i := 0; i < len(kids); i++ {
			for j := i + 1; j < len(kids); j++ {
				if byID[kids[j]].Timestamp < byID[kids[i]].Timestamp {
					kids[i], kids[j] = kids[j], kids[i]
				}
			}
		}
		for _, cid := range kids {
			n.Children = append(n.Children, build(cid))
		}
		return n
	}
	out := make([]TreeNode, 0, len(rootIDs))
	for _, id := range rootIDs {
		out = append(out, build(id))
	}
	return out
}

// CreateBranchedSession writes a new session file containing only the path from
// root to leafID.
func (m *Manager) CreateBranchedSession(leafID, cwd, agentDir string) (*Manager, error) {
	path := m.GetBranch(leafID)
	if len(path) == 0 {
		return nil, fmt.Errorf("entry %s not found", leafID)
	}
	child := New(cwd, agentDir)
	child.header.ParentSession = m.file
	copied := make([]*Entry, 0, len(path))
	for i, e := range path {
		ce := e
		if i == 0 {
			ce.ParentID = nil
		} else {
			prev := copied[i-1].ID
			ce.ParentID = &prev
		}
		copied = append(copied, &ce)
	}
	child.entries = copied
	child.leafID = copied[len(copied)-1].ID
	if err := child.flushCopied(); err != nil {
		return nil, err
	}
	return child, nil
}

func (m *Manager) flushCopied() error {
	if !m.persist {
		return nil
	}
	hasAssistant := false
	for _, e := range m.entries {
		if entryRole(e) == "assistant" {
			hasAssistant = true
			break
		}
	}
	if !hasAssistant {
		return nil
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(m.file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := writeLine(f, m.header); err != nil {
		return err
	}
	for _, e := range m.entries {
		if err := writeLine(f, e); err != nil {
			return err
		}
	}
	m.flushed = true
	return nil
}

func entryRole(e *Entry) string {
	if e == nil {
		return ""
	}
	if e.role != "" {
		return e.role
	}
	var p struct {
		Role string `json:"role"`
	}
	_ = json.Unmarshal(e.Message, &p)
	return p.Role
}

// ForkFrom creates a child session branched before (or at) entryID.
// position "at" keeps the entry (/clone); "before" drops it and returns its text (/fork).
func (m *Manager) ForkFrom(entryID, cwd, agentDir, position string) (*Manager, string, error) {
	var target *Entry
	for _, e := range m.entries {
		if e != nil && e.ID == entryID {
			target = e
			break
		}
	}
	if target == nil {
		return nil, "", fmt.Errorf("invalid entry ID for forking")
	}
	if position == "at" {
		child, err := m.CreateBranchedSession(entryID, cwd, agentDir)
		return child, "", err
	}
	text := userText(target)
	if target.ParentID == nil || *target.ParentID == "" {
		child := New(cwd, agentDir)
		child.header.ParentSession = m.file
		return child, text, nil
	}
	child, err := m.CreateBranchedSession(*target.ParentID, cwd, agentDir)
	return child, text, err
}

func userText(e *Entry) string {
	if e == nil {
		return ""
	}
	var p struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	if json.Unmarshal(e.Message, &p) != nil {
		return ""
	}
	switch c := p.Content.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, item := range c {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			if m["type"] == "text" {
				t, _ := m["text"].(string)
				b.WriteString(t)
			}
		}
		return b.String()
	default:
		return ""
	}
}

// UserMessagesForForking lists user turns for the fork picker.
func (m *Manager) UserMessagesForForking() []map[string]string {
	var out []map[string]string
	for _, e := range m.entries {
		if e == nil || (e.Type != "message" && e.Type != "") {
			continue
		}
		var p struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}
		if json.Unmarshal(e.Message, &p) != nil || p.Role != "user" {
			continue
		}
		text := ""
		if s, ok := p.Content.(string); ok {
			text = s
		}
		if text == "" {
			continue
		}
		out = append(out, map[string]string{"entryId": e.ID, "text": text})
	}
	return out
}

// FormatTree renders a plain-text dump of the parentId tree for /tree.
func (m *Manager) FormatTree() string {
	var b strings.Builder
	leaf := m.leafID
	var walk func(TreeNode, string)
	walk = func(n TreeNode, indent string) {
		mark := " "
		if n.Entry.ID == leaf {
			mark = "*"
		}
		role := entryRole(&n.Entry)
		snippet := userText(&n.Entry)
		if snippet == "" {
			snippet = role
		}
		if len(snippet) > 80 {
			snippet = snippet[:80] + "…"
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(&b, "%s%s %s %s\n", indent, mark, shortID(n.Entry.ID), snippet)
		for _, c := range n.Children {
			walk(c, indent+"  ")
		}
	}
	for _, n := range m.GetTree() {
		walk(n, "")
	}
	if b.Len() == 0 {
		return "(empty session tree)\n"
	}
	return b.String()
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Summaries lists sessions for cwd, newest first, with a first-message preview.
func Summaries(cwd, agentDir string) ([]Summary, error) {
	paths, err := List(cwd, agentDir)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(paths))
	for _, p := range paths {
		h, entries, err := Load(p)
		if err != nil {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		first := ""
		for _, e := range entries {
			if t := userText(&e); t != "" {
				first = t
				break
			}
		}
		if len(first) > 60 {
			first = first[:60] + "…"
		}
		first = strings.ReplaceAll(first, "\n", " ")
		out = append(out, Summary{
			Path: p, ID: h.ID, Name: displayName(h, entries), Cwd: h.Cwd, FirstMessage: first, Modified: info.ModTime(),
		})
	}
	return out, nil
}

func displayName(h Header, entries []Entry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == "session_info" && entries[i].Name != "" {
			return entries[i].Name
		}
	}
	return h.Name
}

// SummariesAll lists sessions under agentDir/sessions, newest first.
func SummariesAll(agentDir string) ([]Summary, error) {
	root := filepath.Join(agentDir, "sessions")
	ents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Summary
	for _, d := range ents {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(root, d.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".jsonl" {
				continue
			}
			p := filepath.Join(dir, f.Name())
			h, entries, err := Load(p)
			if err != nil {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			first := ""
			for _, e := range entries {
				if t := userText(&e); t != "" {
					first = t
					break
				}
			}
			if len(first) > 60 {
				first = first[:60] + "…"
			}
			first = strings.ReplaceAll(first, "\n", " ")
			out = append(out, Summary{
				Path: p, ID: h.ID, Name: displayName(h, entries), Cwd: h.Cwd, FirstMessage: first, Modified: info.ModTime(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}
