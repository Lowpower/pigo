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

// Resource is one discovered extensions/skills/prompts/themes path.
type Resource struct {
	Path    string
	Enabled bool
	Scope   string // user | project
	Origin  string // package | top-level
	Source  string
	Type    string // extensions | skills | prompts | themes
	BaseDir string
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
	if len(manifest.Extensions) > 0 {
		var entries []string
		for _, rel := range manifest.Extensions {
			if isOverridePattern(rel) {
				continue
			}
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

func readPiManifest(packageJSON string) piManifest {
	b, err := os.ReadFile(packageJSON)
	if err != nil {
		return piManifest{}
	}
	var pkg struct {
		Pi piManifest `json:"pi"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return piManifest{}
	}
	return pkg.Pi
}

type piManifest struct {
	Extensions []string `json:"extensions"`
	Skills     []string `json:"skills"`
	Prompts    []string `json:"prompts"`
	Themes     []string `json:"themes"`
}

func (m piManifest) entries(kind string) []string {
	switch kind {
	case KindExtensions:
		return m.Extensions
	case KindSkills:
		return m.Skills
	case KindPrompts:
		return m.Prompts
	case KindThemes:
		return m.Themes
	default:
		return nil
	}
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

func collectPackageFiles(packageRoot, kind string) []string {
	if kind == KindExtensions {
		if st, err := os.Stat(packageRoot); err == nil && st.Mode().IsRegular() {
			return []string{packageRoot}
		}
		return collectPackageExtensionFiles(packageRoot)
	}
	man := readPiManifest(filepath.Join(packageRoot, "package.json"))
	if ents := man.entries(kind); len(ents) > 0 {
		var out []string
		for _, rel := range ents {
			if isOverridePattern(rel) {
				continue
			}
			p := filepath.Join(packageRoot, rel)
			if st, err := os.Stat(p); err == nil {
				if st.IsDir() {
					out = append(out, collectResourceFiles(p, kind)...)
				} else {
					out = append(out, p)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	dir := filepath.Join(packageRoot, kind)
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return collectResourceFiles(dir, kind)
	}
	return nil
}

func collectResourceFiles(dir, kind string) []string {
	switch kind {
	case KindExtensions:
		return collectAutoExtensionEntries(dir)
	case KindSkills:
		return collectSkillEntries(dir)
	case KindPrompts:
		return collectFilesByExt(dir, ".md")
	case KindThemes:
		return collectFilesByExt(dir, ".json")
	default:
		return nil
	}
}

func collectSkillEntries(dir string) []string {
	return collectSkillEntriesAt(dir, dir)
}

func collectSkillEntriesAt(dir, root string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range ents {
		if e.Name() != "SKILL.md" {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			return []string{full}
		}
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
			if strings.HasSuffix(name, ".md") && dir == root {
				out = append(out, full)
			}
			continue
		}
		if info.IsDir() {
			out = append(out, collectSkillEntriesAt(full, root)...)
		}
	}
	return out
}

func collectFilesByExt(dir, ext string) []string {
	var out []string
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
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
			if strings.HasSuffix(strings.ToLower(name), ext) {
				out = append(out, full)
			}
			continue
		}
		if info.IsDir() {
			out = append(out, collectFilesByExt(full, ext)...)
		}
	}
	return out
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
		if r.Type != "" && r.Type != KindExtensions {
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
