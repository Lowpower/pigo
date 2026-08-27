package skills

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Skill is one SKILL.md document.
type Skill struct {
	Name        string
	Description string
	FilePath    string
	Body        string
	DisableLLM  bool
	Source      string // user | project | extra
}

// Discover walks the skill directories and extra paths.
func Discover(cwd, agentDir string, extra []string, includeDefaults, includeProject bool) ([]Skill, error) {
	var dirs []dirSrc
	if includeDefaults {
		dirs = append(dirs,
			dirSrc{filepath.Join(agentDir, "skills"), "user"},
		)
		if includeProject && cwd != "" {
			dirs = append(dirs, dirSrc{filepath.Join(cwd, ".pigo", "skills"), "project"})
		}
	}
	for _, p := range extra {
		dirs = append(dirs, dirSrc{p, "extra"})
	}
	seen := map[string]bool{}
	var out []Skill
	for _, d := range dirs {
		found, err := walk(d.path, d.source)
		if err != nil {
			continue
		}
		for _, s := range found {
			key := strings.ToLower(s.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, s)
		}
	}
	return out, nil
}

type dirSrc struct {
	path, source string
}

func walk(root, source string) ([]Skill, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	var skills []Skill
	if !info.IsDir() {
		if s, ok, err := loadFile(root, source); err == nil && ok {
			skills = append(skills, s)
		}
		return skills, nil
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == "node_modules" || (strings.HasPrefix(base, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.EqualFold(name, "SKILL.md") && !strings.HasSuffix(strings.ToLower(name), ".md") {
			return nil
		}
		s, ok, err := loadFile(path, source)
		if err != nil || !ok {
			return nil
		}
		skills = append(skills, s)
		if strings.EqualFold(name, "SKILL.md") {
			return filepath.SkipDir
		}
		return nil
	})
	return skills, err
}

func loadFile(path, source string) (Skill, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, false, err
	}
	fm, body := ParseFrontmatter(string(b))
	desc := fm["description"]
	if desc == "" {
		return Skill{}, false, nil
	}
	name := fm["name"]
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
		if strings.EqualFold(filepath.Base(path), "SKILL.md") && name == "." {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
	}
	name = strings.ToLower(name)
	if !validName(name) {
		return Skill{}, false, nil
	}
	return Skill{
		Name:        name,
		Description: desc,
		FilePath:    path,
		Body:        body,
		DisableLLM:  fm["disable-model-invocation"] == "true",
		Source:      source,
	}, true, nil
}

func validName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	if name[0] == '-' || name[len(name)-1] == '-' || strings.Contains(name, "--") {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// ParseFrontmatter splits optional YAML --- frontmatter from a markdown body.
func ParseFrontmatter(s string) (map[string]string, string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return map[string]string{}, s
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return map[string]string{}, s
	}
	fm := parseYAMLMap(rest[:end])
	body := rest[end+5:]
	return fm, strings.TrimSpace(body)
}

func parseYAMLMap(s string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		out[strings.TrimSpace(k)] = v
	}
	return out
}

// FormatForPrompt renders the <available_skills> XML block.
func FormatForPrompt(skills []Skill) string {
	var b strings.Builder
	b.WriteString("<available_skills>\n")
	for _, s := range skills {
		if s.DisableLLM {
			continue
		}
		b.WriteString(`<skill name="`)
		b.WriteString(s.Name)
		b.WriteString(`" location="`)
		b.WriteString(s.FilePath)
		b.WriteString(`">`)
		b.WriteString(s.Description)
		b.WriteString("</skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

// ExpandCommand turns `/skill:name args` into the XML payload injected into the prompt.
func ExpandCommand(skills []Skill, name, args string) (string, bool) {
	name = strings.TrimPrefix(strings.ToLower(name), "skill:")
	for _, s := range skills {
		if s.Name == name {
			var b strings.Builder
			b.WriteString(`<skill name="`)
			b.WriteString(s.Name)
			b.WriteString(`" location="`)
			b.WriteString(s.FilePath)
			b.WriteString("\">\n")
			b.WriteString(s.Body)
			if args != "" {
				b.WriteString("\n\narguments: ")
				b.WriteString(args)
			}
			b.WriteString("\n</skill>")
			return b.String(), true
		}
	}
	return "", false
}
