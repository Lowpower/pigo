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

var trustRequiring = []string{
	"settings.json", "extensions", "skills", "prompts", "themes",
	"SYSTEM.md", "APPEND_SYSTEM.md", "npm", "git", "sandbox.json",
}

// HasProjectResources reports whether cwd has project-local resources that
// require a trust decision: entries under cwd/.pigo, or .agents/skills in cwd
// or an ancestor (never ~/.agents/skills).
func HasProjectResources(cwd string) bool {
	root := filepath.Join(cwd, ".pigo")
	for _, n := range trustRequiring {
		if _, err := os.Stat(filepath.Join(root, n)); err == nil {
			return true
		}
	}
	home, _ := os.UserHomeDir()
	userAgents := filepath.Join(home, ".agents", "skills")
	cur, err := filepath.Abs(cwd)
	if err != nil {
		cur = cwd
	}
	for {
		agents := filepath.Join(cur, ".agents", "skills")
		if agents != userAgents {
			if _, err := os.Stat(agents); err == nil {
				return true
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}

// Options controls Decide besides the saved store and CLI override.
type Options struct {
	Override *bool
	Default  string // ask (default), always, never
}

// Resolve combines an optional CLI override with the saved store.
func Resolve(store *Store, cwd string, override *bool) bool {
	return Decide(store, cwd, Options{Override: override})
}

// Decide is the project-trust resolution order:
// override → no project resources → saved store → defaultProjectTrust (ask/always/never).
// "ask" without a saved decision is untrusted (print/json/rpc, or TUI until /trust).
func Decide(store *Store, cwd string, opts Options) bool {
	if opts.Override != nil {
		return *opts.Override
	}
	if !HasProjectResources(cwd) {
		return true
	}
	if store != nil {
		if v, ok := store.Get(cwd); ok {
			return v
		}
	}
	switch opts.Default {
	case "always":
		return true
	case "never":
		return false
	default:
		return false
	}
}

// UntrustedHint is shown in the TUI when project resources exist but are gated.
const UntrustedHint = "project is not trusted; use /trust then restart to load project resources"
