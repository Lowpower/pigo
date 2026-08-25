package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
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
// new one if none exist (pi --continue).
func ContinueRecent(cwd, agentDir string) (*Manager, error) {
	paths, err := listSessionFiles(cwd, agentDir)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return New(cwd, agentDir), nil
	}
	return Open(paths[0])
}

// List returns session file paths for cwd, newest first.
func List(cwd, agentDir string) ([]string, error) {
	return listSessionFiles(cwd, agentDir)
}

func listSessionFiles(cwd, agentDir string) ([]string, error) {
	resolved, err := filepath.Abs(cwd)
	if err != nil {
		resolved = cwd
	}
	dir := sessionDir(agentDir, resolved)
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
		files = append(files, fileInfo{path: p, mod: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.path
	}
	return out, nil
}

// Fork duplicates the current branch into a new session file (pi /clone:
// createBranchedSession at the current leaf). parentSession is the source path.
func (m *Manager) Fork(cwd, agentDir string) (*Manager, error) {
	leaf := m.leafID
	if leaf == "" && len(m.entries) > 0 {
		leaf = m.entries[len(m.entries)-1].ID
	}
	if leaf == "" {
		child := New(cwd, agentDir)
		child.header.ParentSession = m.file
		return child, nil
	}
	return m.CreateBranchedSession(leaf, cwd, agentDir)
}

// AppendCompaction records a compaction summary entry (pi session entry type).
func (m *Manager) AppendCompaction(summary string) (*Entry, error) {
	raw, err := json.Marshal(map[string]any{
		"type":      "compaction",
		"summary":   summary,
		"timestamp": isoNow(),
	})
	if err != nil {
		return nil, err
	}
	e := &Entry{Type: "compaction", ID: newUUID(), Timestamp: isoNow(), Message: raw, role: "assistant"}
	if m.leafID != "" {
		prev := m.leafID
		e.ParentID = &prev
	}
	m.entries = append(m.entries, e)
	m.leafID = e.ID
	if err := m.persistEntry(e); err != nil {
		return nil, err
	}
	return e, nil
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
