package tools

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ignoreGitRequired matches ripgrep's default: .gitignore applies only inside a git repo.
// ignoreNoRequireGit matches fd --no-require-git: .gitignore applies even outside a repo.
type ignoreMode int

const (
	ignoreGitRequired ignoreMode = iota
	ignoreNoRequireGit
)

type ignorer struct {
	searchRoot string
	mode       ignoreMode
	cache      map[string][]giPattern
}

type giPattern struct {
	negate  bool
	dirOnly bool
	g       *globMatcher
}

func newIgnorer(searchRoot string, mode ignoreMode) *ignorer {
	abs, err := filepath.Abs(searchRoot)
	if err != nil {
		abs = searchRoot
	}
	return &ignorer{searchRoot: abs, mode: mode, cache: map[string][]giPattern{}}
}

func (ig *ignorer) skipDir(path string, d fs.DirEntry) bool {
	if d.Name() == ".git" && path != ig.searchRoot {
		return true
	}
	return ig.ignored(path, true)
}

func (ig *ignorer) ignored(path string, isDir bool) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if abs == ig.searchRoot {
		return false
	}
	ruleRoot := ig.ruleRootFor(abs)
	if ruleRoot == "" {
		return false
	}
	ignored := false
	for _, dir := range dirsFromRoot(ruleRoot, filepath.Dir(abs)) {
		rel, relErr := filepath.Rel(dir, abs)
		if relErr != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || strings.HasPrefix(rel, "../") {
			continue
		}
		for _, p := range ig.loadDir(dir, dir == ruleRoot) {
			if p.dirOnly && !isDir {
				continue
			}
			if p.g.Match(rel) {
				ignored = !p.negate
			}
		}
	}
	return ignored
}

func (ig *ignorer) ruleRootFor(abs string) string {
	// Nested repo directories are ignored (or not) by the parent tree's rules.
	if git := nearestGitRoot(filepath.Dir(abs)); git != "" {
		return git
	}
	if ig.mode == ignoreNoRequireGit {
		return ig.searchRoot
	}
	return ""
}

func (ig *ignorer) loadDir(dir string, includeExclude bool) []giPattern {
	if cached, ok := ig.cache[dir]; ok {
		return cached
	}
	var pats []giPattern
	if b, err := os.ReadFile(filepath.Join(dir, ".gitignore")); err == nil {
		pats = append(pats, parseGitignore(string(b))...)
	}
	if includeExclude {
		if b, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude")); err == nil {
			pats = append(pats, parseGitignore(string(b))...)
		}
	}
	ig.cache[dir] = pats
	return pats
}

func dirsFromRoot(root, end string) []string {
	rel, err := filepath.Rel(root, end)
	if err != nil {
		return []string{root}
	}
	rel = filepath.ToSlash(rel)
	out := []string{root}
	if rel == "." || strings.HasPrefix(rel, "../") {
		return out
	}
	acc := root
	for _, part := range strings.Split(rel, "/") {
		if part == "" {
			continue
		}
		acc = filepath.Join(acc, part)
		out = append(out, acc)
	}
	return out
}

func nearestGitRoot(path string) string {
	cur, err := filepath.Abs(path)
	if err != nil {
		cur = path
	}
	for {
		info, err := os.Stat(filepath.Join(cur, ".git"))
		if err == nil && info != nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func parseGitignore(content string) []giPattern {
	var out []giPattern
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimRight(sc.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negate := false
		if strings.HasPrefix(line, "!") {
			negate = true
			line = line[1:]
			if line == "" {
				continue
			}
		}
		dirOnly := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			continue
		}
		anchored := strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		if !anchored {
			line = "**/" + line
		}
		g, err := compileGlob(line)
		if err != nil {
			continue
		}
		out = append(out, giPattern{negate: negate, dirOnly: dirOnly, g: g})
	}
	return out
}

func walkUnignored(root string, mode ignoreMode, fn func(path, rel string) error) error {
	ig := newIgnorer(root, mode)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if ig.skipDir(path, d) {
				return filepath.SkipDir
			}
			return nil
		}
		if ig.ignored(path, false) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		return fn(path, filepath.ToSlash(rel))
	})
}
