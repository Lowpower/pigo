package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type sessionFile struct {
	path string
	mod  time.Time
}

// ListAll returns every session jsonl under agentDir/sessions, newest first.
// Directory symlinks are followed. The returned path keeps the alias that was
// walked, and the same physical file is listed once.
func ListAll(agentDir string) ([]string, error) {
	root := filepath.Join(agentDir, "sessions")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}
	seenDirs := map[string]struct{}{}
	seenFiles := map[string]struct{}{}
	var files []sessionFile
	var walk func(dir string) error
	walk = func(dir string) error {
		canon, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return nil
		}
		canon = filepath.Clean(canon)
		if _, ok := seenDirs[canon]; ok {
			return nil
		}
		seenDirs[canon] = struct{}{}
		ents, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, e := range ents {
			p := filepath.Join(dir, e.Name())
			if e.Type()&os.ModeSymlink != 0 {
				info, statErr := os.Stat(p)
				if statErr != nil {
					continue
				}
				if info.IsDir() {
					if err := walk(p); err != nil {
						return err
					}
					continue
				}
				if filepath.Ext(p) == ".jsonl" {
					addSessionFile(&files, seenFiles, p, info.ModTime())
				}
				continue
			}
			if e.IsDir() {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			if filepath.Ext(p) != ".jsonl" {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			addSessionFile(&files, seenFiles, p, info.ModTime())
		}
		return nil
	}
	if err := walk(root); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.path
	}
	return out, nil
}

func addSessionFile(files *[]sessionFile, seen map[string]struct{}, path string, mod time.Time) {
	key := pathKey(path)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*files = append(*files, sessionFile{path: path, mod: mod})
}

// pathKey is the physical path when it can be resolved, otherwise the cleaned path.
func pathKey(p string) string {
	if p == "" {
		return ""
	}
	if c, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(c)
	}
	return filepath.Clean(p)
}

// SamePath reports whether a and b name the same file.
// Both paths are canonicalized when that succeeds; otherwise comparison uses
// the cleaned original strings.
func SamePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ca, ea := filepath.EvalSymlinks(a)
	cb, eb := filepath.EvalSymlinks(b)
	if ea == nil && eb == nil {
		return filepath.Clean(ca) == filepath.Clean(cb)
	}
	return filepath.Clean(a) == filepath.Clean(b)
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
	if SamePath(path, current) {
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
