package session

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NavigateOpts controls in-session tree navigation (no new session file).
type NavigateOpts struct {
	Summarize           bool
	CustomInstructions  string
	ReplaceInstructions bool
	Label               string
	Summary             string // precomputed summary text; empty skips branch_summary
	FromHook            bool
}

// NavigateResult is the outcome of Navigate.
type NavigateResult struct {
	EditorText string
	Cancelled  bool
	Aborted    bool
	NewLeafID  string
	OldLeafID  string
}

// TreePrep is the snapshot passed to BeforeTree.
type TreePrep struct {
	TargetID            string
	OldLeafID           string
	CommonAncestorID    string
	EntriesToSummarize  []Entry
	UserWantsSummary    bool
	CustomInstructions  string
	ReplaceInstructions bool
	Label               string
}

// TreeHookResult is returned by BeforeTree. Cancel skips navigation.
type TreeHookResult struct {
	Cancel              bool
	Summary             string
	CustomInstructions  string
	ReplaceInstructions *bool
	Label               *string
}

// AbandonedBranch is the path from oldLeaf back to (but not including) the
// common ancestor with target.
func AbandonedBranch(m *Manager, oldLeafID, targetID string) (entries []Entry, commonAncestorID string) {
	if oldLeafID == "" {
		return nil, ""
	}
	oldPath := m.GetBranch(oldLeafID)
	oldIDs := map[string]bool{}
	for _, e := range oldPath {
		oldIDs[e.ID] = true
	}
	targetPath := m.GetBranch(targetID)
	for i := len(targetPath) - 1; i >= 0; i-- {
		if oldIDs[targetPath[i].ID] {
			commonAncestorID = targetPath[i].ID
			break
		}
	}
	byID := map[string]*Entry{}
	for _, e := range m.entries {
		if e != nil {
			byID[e.ID] = e
		}
	}
	var rev []Entry
	cur := byID[oldLeafID]
	seen := map[string]bool{}
	for cur != nil && !seen[cur.ID] && cur.ID != commonAncestorID {
		seen[cur.ID] = true
		rev = append(rev, *cur)
		if cur.ParentID == nil || *cur.ParentID == "" {
			break
		}
		cur = byID[*cur.ParentID]
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, commonAncestorID
}

// Navigate moves the leaf so the next append is a child of the selected point.
func (m *Manager) Navigate(targetID string, opts NavigateOpts) (NavigateResult, error) {
	oldLeaf := m.leafID
	if targetID == oldLeaf {
		return NavigateResult{OldLeafID: oldLeaf, NewLeafID: oldLeaf}, nil
	}
	target, ok := m.EntryByID(targetID)
	if !ok {
		return NavigateResult{}, fmt.Errorf("entry %s not found", targetID)
	}

	newLeaf := targetID
	editor := ""
	if isEditableMessage(target) {
		if target.ParentID == nil || *target.ParentID == "" {
			newLeaf = ""
		} else {
			newLeaf = *target.ParentID
		}
		editor = EntryContentText(target)
	}

	if opts.Summary != "" {
		if err := m.Branch(newLeaf); err != nil {
			return NavigateResult{}, err
		}
		from := oldLeaf
		if from == "" {
			from = "root"
		}
		sum, err := m.AppendBranchSummary(from, opts.Summary, opts.FromHook)
		if err != nil {
			return NavigateResult{}, err
		}
		if opts.Label != "" {
			if _, err := m.AppendLabel(sum.ID, opts.Label); err != nil {
				return NavigateResult{}, err
			}
		}
		return NavigateResult{EditorText: editor, NewLeafID: m.leafID, OldLeafID: oldLeaf}, nil
	}

	if err := m.Branch(newLeaf); err != nil {
		return NavigateResult{}, err
	}
	if opts.Label != "" {
		if _, err := m.AppendLabel(targetID, opts.Label); err != nil {
			return NavigateResult{}, err
		}
	}
	return NavigateResult{EditorText: editor, NewLeafID: m.leafID, OldLeafID: oldLeaf}, nil
}

func isEditableMessage(e Entry) bool {
	if e.Type == "custom_message" {
		return true
	}
	if e.Type != "message" && e.Type != "" {
		return false
	}
	return entryRole(&e) == "user"
}

// AppendLabel records a type:label entry targeting id. Empty label clears it.
func (m *Manager) AppendLabel(targetID, label string) (*Entry, error) {
	if _, ok := m.EntryByID(targetID); !ok {
		return nil, fmt.Errorf("entry %s not found", targetID)
	}
	e := &Entry{
		Type:      "label",
		ID:        newUUID(),
		Timestamp: isoNow(),
		TargetID:  targetID,
	}
	if strings.TrimSpace(label) != "" {
		l := label
		e.Label = &l
	}
	return m.appendEntry(e)
}

// AppendBranchSummary writes a branch_summary child of the current leaf.
func (m *Manager) AppendBranchSummary(fromID, summary string, fromHook bool) (*Entry, error) {
	e := &Entry{
		Type:      "branch_summary",
		ID:        newUUID(),
		Timestamp: isoNow(),
		FromID:    fromID,
		Summary:   summary,
		role:      "assistant",
	}
	if fromHook {
		raw, _ := json.Marshal(map[string]any{"fromHook": true})
		e.Details = raw
	}
	return m.appendEntry(e)
}

// AppendSessionInfo records a display-name change.
func (m *Manager) AppendSessionInfo(name string) (*Entry, error) {
	e := &Entry{
		Type:      "session_info",
		ID:        newUUID(),
		Timestamp: isoNow(),
		Name:      name,
	}
	return m.appendEntry(e)
}

// ContextEntries is the leaf path used for LLM context (latest compaction wins).
func ContextEntries(m *Manager) []Entry {
	if m == nil {
		return nil
	}
	path := m.GetBranch("")
	last := -1
	for i, e := range path {
		if e.Type == "compaction" {
			last = i
		}
	}
	if last >= 0 {
		return path[last:]
	}
	return path
}

// EntryContentText is the text shown in the editor when navigating to a user turn.
func EntryContentText(e Entry) string {
	if e.Type == "custom_message" {
		if e.Summary != "" {
			return e.Summary
		}
	}
	return userText(&e)
}

func resolvedLabels(entries []Entry) (map[string]string, map[string]string) {
	labels := map[string]string{}
	ts := map[string]string{}
	for _, e := range entries {
		if e.Type != "label" || e.TargetID == "" {
			continue
		}
		if e.Label == nil || strings.TrimSpace(*e.Label) == "" {
			delete(labels, e.TargetID)
			delete(ts, e.TargetID)
			continue
		}
		labels[e.TargetID] = *e.Label
		ts[e.TargetID] = e.Timestamp
	}
	return labels, ts
}
