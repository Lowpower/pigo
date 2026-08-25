// Package pkgmgr installs, removes, and discovers coding-agent packages.
package pkgmgr

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Resource is one discovered extension path.
type Resource struct {
	Path    string
	Enabled bool
	Scope   string // user | project
	Origin  string // package | top-level
	Source  string
}

func collectAutoExtensionEntries(dir string) []string {
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	if root := resolveExtensionEntries(dir); root != nil {
		return root
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := e.Info()
		if err != nil {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			info, err = os.Stat(full)
			if err != nil {
				continue
			}
		}
		if info.Mode().IsRegular() {
			if strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".js") || IsSpawnable(full) {
				out = append(out, full)
			}
			continue
		}
		if info.IsDir() {
			if resolved := resolveExtensionEntries(full); resolved != nil {
				out = append(out, resolved...)
				continue
			}
			sub, err := os.ReadDir(full)
			if err != nil {
				continue
			}
			for _, se := range sub {
				sp := filepath.Join(full, se.Name())
				if IsSpawnable(sp) {
					out = append(out, sp)
				}
			}
		}
	}
	return out
}

func resolveExtensionEntries(dir string) []string {
	manifest := readPiManifest(filepath.Join(dir, "package.json"))
	if len(manifest) > 0 {
		var entries []string
		for _, rel := range manifest {
			p := filepath.Join(dir, rel)
			if _, err := os.Stat(p); err == nil {
				entries = append(entries, p)
			}
		}
		if len(entries) > 0 {
			return entries
		}
	}
	indexTS := filepath.Join(dir, "index.ts")
	indexJS := filepath.Join(dir, "index.js")
	if _, err := os.Stat(indexTS); err == nil {
		return []string{indexTS}
	}
	if _, err := os.Stat(indexJS); err == nil {
		return []string{indexJS}
	}
	return nil
}

func readPiManifest(packageJSON string) []string {
	b, err := os.ReadFile(packageJSON)
	if err != nil {
		return nil
	}
	var pkg struct {
		Pi struct {
			Extensions []string `json:"extensions"`
		} `json:"pi"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return nil
	}
	return pkg.Pi.Extensions
}

func collectPackageExtensionFiles(packageRoot string) []string {
	if entries := resolveExtensionEntries(packageRoot); entries != nil {
		return entries
	}
	extDir := filepath.Join(packageRoot, "extensions")
	if st, err := os.Stat(extDir); err == nil && st.IsDir() {
		return collectAutoExtensionEntries(extDir)
	}
	return nil
}

// IsSpawnable reports whether path can be started as an extension subprocess.
func IsSpawnable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if info.Mode()&0o111 != 0 {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	b, err := r.Peek(2)
	if err != nil && err != io.EOF {
		return false
	}
	return len(b) >= 2 && b[0] == '#' && b[1] == '!'
}

// SpawnArgv returns argv lists for spawnable resources (enabled only).
func SpawnArgv(rs []Resource) [][]string {
	var out [][]string
	seen := map[string]bool{}
	for _, r := range rs {
		if !r.Enabled {
			continue
		}
		if !IsSpawnable(r.Path) {
			continue
		}
		if seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		out = append(out, []string{r.Path})
	}
	return out
}
