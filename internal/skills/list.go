package skills

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ListCandidates returns skill files under root, in directory order.
// Symlinks to directories and files are followed. A broken symlink is skipped.
// The same canonical file is returned once. Names starting with "." and
// node_modules are not entered.
//
// .gitignore, .ignore, and .fdignore files at root and below apply, in that
// order. Later matching patterns win, including ! negation. Ignore files above
// root do not apply. An ignored directory is not entered.
//
// A directory that contains a regular SKILL.md, including a symlink to a file,
// contributes only that file. Otherwise, direct .md children are included only
// when allowRootMarkdown is true. Subdirectories are searched only for SKILL.md.
// allowRootMarkdown is true for ~/.pigo/agent/skills, .pigo/skills, and package
// skills directories, and false for .agents/skills.
func ListCandidates(root string, allowRootMarkdown bool) []string {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	st := &listState{seenDir: map[string]struct{}{}, seenFile: map[string]struct{}{}}
	st.walk(root, nil, allowRootMarkdown)
	return st.out
}

type listState struct {
	seenDir  map[string]struct{}
	seenFile map[string]struct{}
	out      []string
}

func (st *listState) walk(dir string, parent []ignoreRule, allowMD bool) {
	resolved := canonicalPath(dir)
	if _, ok := st.seenDir[resolved]; ok {
		return
	}
	st.seenDir[resolved] = struct{}{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	rules := append([]ignoreRule{}, parent...)
	if extra := loadIgnore(dir); len(extra.pats) > 0 {
		rules = append(rules, extra)
	}
	if skill, ok := skillMarkdown(dir, entries); ok {
		if !ignored(skill, false, rules) {
			st.add(skill)
		}
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if ignored(full, true, rules) {
				continue
			}
			st.walk(full, rules, false)
			continue
		}
		if allowMD && info.Mode().IsRegular() && strings.HasSuffix(name, ".md") && !ignored(full, false, rules) {
			st.add(full)
		}
	}
}

func (st *listState) add(path string) {
	key := canonicalPath(path)
	if _, ok := st.seenFile[key]; ok {
		return
	}
	st.seenFile[key] = struct{}{}
	st.out = append(st.out, path)
}

// skillMarkdown reports the SKILL.md in dir when it follows to a regular file.
func skillMarkdown(dir string, entries []os.DirEntry) (string, bool) {
	for _, e := range entries {
		if e.Name() != "SKILL.md" {
			continue
		}
		full := filepath.Join(dir, e.Name())
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			return "", false
		}
		return full, true
	}
	return "", false
}

func standaloneMarkdownRoot(root string) bool {
	clean := filepath.Clean(root)
	return filepath.Base(clean) != "skills" || filepath.Base(filepath.Dir(clean)) != ".agents"
}

type ignoreRule struct {
	dir  string
	pats []ignorePat
}

type ignorePat struct {
	negate  bool
	dirOnly bool
	parts   []string
}

func loadIgnore(dir string) ignoreRule {
	var pats []ignorePat
	for _, name := range []string{".gitignore", ".ignore", ".fdignore"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		pats = append(pats, parseIgnore(string(b))...)
	}
	return ignoreRule{dir: dir, pats: pats}
}

func parseIgnore(content string) []ignorePat {
	var out []ignorePat
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimRight(sc.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negate := false
		if strings.HasPrefix(line, "!") {
			negate = true
			line = strings.TrimSpace(line[1:])
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
		out = append(out, ignorePat{negate: negate, dirOnly: dirOnly, parts: strings.Split(line, "/")})
	}
	return out
}

func ignored(path string, isDir bool, rules []ignoreRule) bool {
	abs := absPath(path)
	state := false
	for _, rule := range rules {
		rel, err := filepath.Rel(absPath(rule.dir), abs)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || strings.HasPrefix(rel, "../") {
			continue
		}
		for _, p := range rule.pats {
			if p.dirOnly && !isDir {
				continue
			}
			if matchParts(p.parts, strings.Split(rel, "/")) {
				state = !p.negate
			}
		}
	}
	return state
}

func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func matchParts(pat, path []string) bool {
	if len(pat) == 0 {
		return len(path) == 0
	}
	if pat[0] == "**" {
		if matchParts(pat[1:], path) {
			return true
		}
		if len(path) == 0 {
			return false
		}
		return matchParts(pat, path[1:])
	}
	if len(path) == 0 || !matchOne(pat[0], path[0]) {
		return false
	}
	return matchParts(pat[1:], path[1:])
}

func matchOne(pat, s string) bool {
	pr := []rune(pat)
	sr := []rune(s)
	var rec func(i, j int) bool
	rec = func(i, j int) bool {
		if i == len(pr) {
			return j == len(sr)
		}
		if pr[i] == '*' {
			for k := j; k <= len(sr); k++ {
				if rec(i+1, k) {
					return true
				}
			}
			return false
		}
		if j == len(sr) {
			return false
		}
		if pr[i] != '?' && pr[i] != sr[j] {
			return false
		}
		return rec(i+1, j+1)
	}
	return rec(0, 0)
}
