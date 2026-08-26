package session

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ListAll returns every session jsonl under agentDir/sessions, newest first.
func ListAll(agentDir string) ([]string, error) {
	root := filepath.Join(agentDir, "sessions")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}
	var files []struct {
		path string
		mod  time.Time
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, struct {
			path string
			mod  time.Time
		}{path, info.ModTime()})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.path
	}
	return out, nil
}

// SummariesAll lists sessions across all projects.
func SummariesAll(agentDir string) ([]Summary, error) {
	paths, err := ListAll(agentDir)
	if err != nil {
		return nil, err
	}
	return summariesFrom(paths)
}

// DeleteFile removes a session jsonl. It refuses to delete path if it is current.
func DeleteFile(path, current string) error {
	if path == "" {
		return os.ErrInvalid
	}
	if current != "" && filepath.Clean(path) == filepath.Clean(current) {
		return os.ErrPermission
	}
	return os.Remove(path)
}

// UpdateHeader rewrites the first jsonl line of a session file.
func UpdateHeader(path string, fn func(*Header)) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(b)
	nl := strings.IndexByte(text, '\n')
	first := text
	rest := ""
	if nl >= 0 {
		first = text[:nl]
		rest = text[nl:]
	}
	var h Header
	if err := json.Unmarshal([]byte(first), &h); err != nil {
		return err
	}
	fn(&h)
	nb, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(nb, []byte(rest)...), 0o644)
}
