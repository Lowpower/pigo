package skills

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
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

const maxDescriptionLen = 1024

// Diagnostic is one skill-load warning. Message is the full startup sentence.
type Diagnostic struct {
	Path    string
	Message string
}

// FormatWarnings returns each diagnostic sentence. An empty input returns nil.
func FormatWarnings(ds []Diagnostic) []string {
	if len(ds) == 0 {
		return nil
	}
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Message
	}
	return out
}

// Discover walks the skill directories and extra paths.
// Project skills are scanned before user skills. The first skill of a name wins.
// A second path to the same file, including a symlink, is skipped with no diagnostic.
func Discover(cwd, agentDir string, extra []string, includeDefaults, includeProject bool) ([]Skill, []Diagnostic) {
	var dirs []dirSrc
	if includeDefaults {
		if includeProject && cwd != "" {
			dirs = append(dirs, dirSrc{filepath.Join(cwd, ".pigo", "skills"), "project"})
		}
		dirs = append(dirs, dirSrc{filepath.Join(agentDir, "skills"), "user"})
	}
	for _, p := range extra {
		dirs = append(dirs, dirSrc{p, "extra"})
	}
	seenName := map[string]Skill{}
	seenReal := map[string]bool{}
	var out []Skill
	var diags []Diagnostic
	for _, d := range dirs {
		found, more := walk(d.path, d.source)
		diags = append(diags, more...)
		for _, s := range found {
			resolved := canonicalPath(s.FilePath)
			if seenReal[resolved] {
				continue
			}
			key := strings.ToLower(s.Name)
			if prev, ok := seenName[key]; ok {
				diags = append(diags, Diagnostic{
					Path:    s.FilePath,
					Message: fmt.Sprintf("Warning: skill %q in %s conflicts with %s", s.Name, s.FilePath, prev.FilePath),
				})
				continue
			}
			seenName[key] = s
			seenReal[resolved] = true
			out = append(out, s)
		}
	}
	return out, diags
}

type dirSrc struct {
	path, source string
}

func walk(root, source string) ([]Skill, []Diagnostic) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, nil
	}
	if !info.IsDir() {
		s, diags := loadFile(root, source)
		if s.FilePath == "" {
			return nil, diags
		}
		return []Skill{s}, diags
	}
	var skills []Skill
	var diags []Diagnostic
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
		s, more := loadFile(path, source)
		diags = append(diags, more...)
		if s.FilePath != "" {
			skills = append(skills, s)
		}
		if strings.EqualFold(name, "SKILL.md") {
			return filepath.SkipDir
		}
		return nil
	})
	return skills, diags
}

func loadFile(path, source string) (Skill, []Diagnostic) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, nil
	}
	text := string(b)
	declared := strings.EqualFold(filepath.Base(path), "SKILL.md")
	if err := validateClosedFrontmatter(text); err != nil {
		if !declared {
			return Skill{}, nil
		}
		return Skill{}, []Diagnostic{{
			Path:    path,
			Message: fmt.Sprintf("Warning: malformed skill frontmatter in %s: %s", path, err.Error()),
		}}
	}
	fm, body := ParseFrontmatter(text)
	desc := strings.TrimSpace(fm["description"])
	if desc == "" {
		if !declared {
			return Skill{}, nil
		}
		return Skill{}, []Diagnostic{{
			Path:    path,
			Message: fmt.Sprintf("Warning: skill in %s has no description", path),
		}}
	}
	name := fm["name"]
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
		if strings.EqualFold(filepath.Base(path), "SKILL.md") && name == "." {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
	}
	name = strings.ToLower(name)
	var diags []Diagnostic
	if !validName(name) {
		diags = append(diags, Diagnostic{
			Path:    path,
			Message: fmt.Sprintf("Warning: skill in %s has invalid name %q", path, name),
		})
	}
	if utf8.RuneCountInString(desc) > maxDescriptionLen {
		diags = append(diags, Diagnostic{
			Path:    path,
			Message: fmt.Sprintf("Warning: skill in %s has a description longer than 1024 characters", path),
		})
	}
	return Skill{
		Name:        name,
		Description: desc,
		FilePath:    path,
		Body:        body,
		DisableLLM:  fm["disable-model-invocation"] == "true",
		Source:      source,
	}, diags
}

// validateClosedFrontmatter rejects YAML that ParseFrontmatter would otherwise
// accept. An unclosed opening fence is not a closed block.
func validateClosedFrontmatter(s string) error {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return nil
	}
	var v any
	return yaml.Unmarshal([]byte(rest[:end]), &v)
}

func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
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
	s = strings.TrimPrefix(s, "\ufeff")
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
// fileReadTool is "read" (default) or "bash".
func FormatForPrompt(skills []Skill, fileReadTool ...string) string {
	tool := "read"
	if len(fileReadTool) > 0 && fileReadTool[0] != "" {
		tool = fileReadTool[0]
	}
	var b strings.Builder
	if tool == "bash" {
		b.WriteString("Use bash to load a skill's file when the task matches its description.\n")
	} else {
		b.WriteString("Use the read tool to load a skill's file when the task matches its description.\n")
	}
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
