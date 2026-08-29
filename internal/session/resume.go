package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
)

// Open loads an existing session file and continues appending to it.
func Open(path string) (*Manager, error) {
	header, entries, err := Load(path)
	if err != nil {
		return nil, err
	}
	copied := make([]*Entry, len(entries))
	for i := range entries {
		e := entries[i]
		copied[i] = &e
	}
	leaf := ""
	if n := len(copied); n > 0 {
		leaf = copied[n-1].ID
	}
	return &Manager{
		agentDir: filepath.Dir(filepath.Dir(filepath.Dir(path))),
		cwd:      header.Cwd,
		id:       header.ID,
		header:   header,
		dir:      filepath.Dir(path),
		file:     path,
		entries:  copied,
		flushed:  true,
		persist:  true,
		leafID:   leaf,
	}, nil
}

// ContinueRecent opens the most recently modified session for cwd, or starts a
// new one if none exist (--continue).
func ContinueRecent(cwd, agentDir string) (*Manager, error) {
	return ContinueRecentAt(cwd, agentDir, "")
}

// ContinueRecentAt is ContinueRecent using an optional session directory override.
func ContinueRecentAt(cwd, agentDir, sessionDir string) (*Manager, error) {
	paths, err := listSessionFilesAt(cwd, agentDir, sessionDir)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return NewAt(cwd, agentDir, sessionDir), nil
	}
	return Open(paths[0])
}

// List returns session file paths for cwd, newest first.
func List(cwd, agentDir string) ([]string, error) {
	return listSessionFiles(cwd, agentDir)
}

func listSessionFiles(cwd, agentDir string) ([]string, error) {
	return listSessionFilesAt(cwd, agentDir, "")
}

func listSessionFilesAt(cwd, agentDir, sessionDir string) ([]string, error) {
	dir := StorageDir(cwd, agentDir, sessionDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	type fileInfo struct {
		path string
		mod  time.Time
	}
	filterCwd := strings.TrimSpace(sessionDir) != "" && dir != StorageDir(cwd, agentDir, "")
	resolvedCwd := cwd
	if abs, err := filepath.Abs(cwd); err == nil {
		resolvedCwd = abs
	}
	var files []fileInfo
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if filterCwd {
			h, _, err := Load(p)
			if err != nil {
				continue
			}
			hc := h.Cwd
			if abs, err := filepath.Abs(hc); err == nil {
				hc = abs
			}
			if filepath.Clean(hc) != filepath.Clean(resolvedCwd) {
				continue
			}
		}
		files = append(files, fileInfo{path: p, mod: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.path
	}
	return out, nil
}

// Fork duplicates the current branch into a new session file (/clone:
// CreateBranchedSession at the current leaf). parentSession is the source path.
func (m *Manager) Fork(cwd, agentDir string) (*Manager, error) {
	leaf := m.leafID
	if leaf == "" && len(m.entries) > 0 {
		leaf = m.entries[len(m.entries)-1].ID
	}
	if leaf == "" {
		child := NewAt(cwd, agentDir, m.dir)
		child.header.ParentSession = m.file
		return child, nil
	}
	return m.CreateBranchedSession(leaf, cwd, agentDir)
}

// CompactionMeta is optional compaction JSONL fields.
type CompactionMeta struct {
	Details  any
	Usage    *ai.Usage
	FromHook bool
}

// AppendCompaction records a compaction summary entry.
func (m *Manager) AppendCompaction(summary, firstKeptEntryID string, tokensBefore int, meta ...CompactionMeta) (*Entry, error) {
	tok := tokensBefore
	e := &Entry{
		Type:             "compaction",
		ID:               newUUID(),
		Timestamp:        isoNow(),
		Summary:          summary,
		FirstKeptEntryID: firstKeptEntryID,
		TokensBefore:     &tok,
		role:             "assistant",
	}
	if len(meta) > 0 {
		extra := meta[0]
		e.FromHook = extra.FromHook
		e.Usage = extra.Usage
		if extra.Details != nil {
			raw, err := json.Marshal(extra.Details)
			if err != nil {
				return nil, err
			}
			e.Details = raw
		}
	}
	return m.appendEntry(e)
}

// AppendCustomEntry records an extension custom entry (not sent to the LLM).
func (m *Manager) AppendCustomEntry(customType string, data any) (*Entry, error) {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	e := &Entry{
		Type:       "custom",
		ID:         newUUID(),
		Timestamp:  isoNow(),
		CustomType: customType,
		Data:       raw,
	}
	return m.appendEntry(e)
}

// AppendCustomMessage records a custom_message that participates in LLM context.
func (m *Manager) AppendCustomMessage(customType, content string, display bool) (*Entry, error) {
	raw, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	d := display
	e := &Entry{
		Type:       "custom_message",
		ID:         newUUID(),
		Timestamp:  isoNow(),
		CustomType: customType,
		Content:    raw,
		Display:    &d,
	}
	return m.appendEntry(e)
}

// RestoreMessages rebuilds a provider-facing transcript from session entries.
func RestoreMessages(entries []Entry) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if e.Type != "message" && e.Type != "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(e.Message, &payload); err != nil {
			continue
		}
		out = append(out, payload)
	}
	return out
}
