// Package trust stores project-trust decisions in agentDir/trust.json.
package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Store is a thin project-trust map stored as agentDir/trust.json.
type Store struct {
	path string
}

// Open reads (or will create) trust.json under agentDir.
func Open(agentDir string) *Store {
	return &Store{path: filepath.Join(agentDir, "trust.json")}
}

// Get walks cwd and its parents for a saved true/false decision.
// ok is false when nothing is stored.
func (s *Store) Get(cwd string) (trusted bool, ok bool) {
	data := s.read()
	cur, err := filepath.Abs(cwd)
	if err != nil {
		cur = cwd
	}
	for {
		if v, exists := data[cur]; exists {
			return v, true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false, false
		}
		cur = parent
	}
}

// Set records a decision for cwd.
func (s *Store) Set(cwd string, trusted bool) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data := s.read()
	cur, err := filepath.Abs(cwd)
	if err != nil {
		cur = cwd
	}
	data[cur] = trusted
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(b, '\n'), 0o644)
}

func (s *Store) read() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

// HasProjectResources reports whether cwd/.pigo has settings or resource dirs
// that require a trust decision.
func HasProjectResources(cwd string) bool {
	root := filepath.Join(cwd, ".pigo")
	names := []string{"settings.json", "extensions", "skills", "prompts", "themes", "SYSTEM.md", "APPEND_SYSTEM.md", "npm", "git"}
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(root, n)); err == nil {
			return true
		}
	}
	return false
}

// Resolve combines an optional CLI override with the saved store.
// No saved decision and no override → untrusted (non-interactive default).
func Resolve(store *Store, cwd string, override *bool) bool {
	if override != nil {
		return *override
	}
	if store == nil {
		return false
	}
	v, ok := store.Get(cwd)
	return ok && v
}
