package prompt

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Lowpower/pigo/internal/skills"
)

// Template is a markdown prompt template loaded as a slash command.
type Template struct {
	Name         string
	Description  string
	ArgumentHint string
	Content      string
	FilePath     string
	Source       string // user | project | extra
}

// DiscoverTemplates loads:
//  1. agentDir/prompts/*.md
//  2. cwd/.pi/prompts/*.md
//  3. extra files or directories
func DiscoverTemplates(cwd, agentDir string, extra []string, includeDefaults bool) []Template {
	var out []Template
	seen := map[string]bool{}
	add := func(list []Template) {
		for _, t := range list {
			key := strings.ToLower(t.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, t)
		}
	}
	if includeDefaults {
		add(loadDir(filepath.Join(agentDir, "prompts"), "user"))
		add(loadDir(filepath.Join(cwd, ".pi", "prompts"), "project"))
	}
	for _, p := range extra {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			add(loadDir(p, "extra"))
			continue
		}
		if t, ok := loadFile(p, "extra"); ok {
			add([]Template{t})
		}
	}
	return out
}

func loadDir(dir, source string) []Template {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Template
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		if t, ok := loadFile(filepath.Join(dir, e.Name()), source); ok {
			out = append(out, t)
		}
	}
	return out
}

func loadFile(path, source string) (Template, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Template{}, false
	}
	fm, body := skills.ParseFrontmatter(string(b))
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	desc := fm["description"]
	if desc == "" {
		for _, line := range strings.Split(body, "\n") {
			if s := strings.TrimSpace(line); s != "" {
				desc = s
				if len(desc) > 60 {
					desc = desc[:60] + "..."
				}
				break
			}
		}
	}
	return Template{
		Name:         name,
		Description:  desc,
		ArgumentHint: fm["argument-hint"],
		Content:      body,
		FilePath:     path,
		Source:       source,
	}, true
}

// ParseCommandArgs splits a rest string with bash-style quotes.
func ParseCommandArgs(s string) []string {
	var args []string
	var cur strings.Builder
	var inQuote rune
	flush := func() {
		if cur.Len() > 0 {
			args = append(args, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
			continue
		}
		if r == '"' || r == '\'' {
			inQuote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' {
			flush()
			continue
		}
		cur.WriteRune(r)
	}
	flush()
	return args
}

var substRe = regexp.MustCompile(`\$\{(\d+|ARGUMENTS|@):-([^}]*)\}|\$\{@:(\d+)(?::(\d+))?\}|\$(ARGUMENTS|@|\d+)`)

// SubstituteArgs expands $1 / $@ / ${1:-default} / ${@:N} / ${@:N:L}.
func SubstituteArgs(content string, args []string) string {
	all := strings.Join(args, " ")
	return substRe.ReplaceAllStringFunc(content, func(match string) string {
		m := substRe.FindStringSubmatch(match)
		if m == nil {
			return match
		}
		if m[1] != "" {
			value := all
			if m[1] != "@" && m[1] != "ARGUMENTS" {
				i, _ := strconv.Atoi(m[1])
				if i >= 1 && i <= len(args) {
					value = args[i-1]
				} else {
					value = ""
				}
			}
			if value != "" {
				return value
			}
			return m[2]
		}
		if m[3] != "" {
			start, _ := strconv.Atoi(m[3])
			start--
			if start < 0 {
				start = 0
			}
			if m[4] != "" {
				n, _ := strconv.Atoi(m[4])
				end := start + n
				if start > len(args) {
					start = len(args)
				}
				if end > len(args) {
					end = len(args)
				}
				if start >= end {
					return ""
				}
				return strings.Join(args[start:end], " ")
			}
			if start > len(args) {
				return ""
			}
			return strings.Join(args[start:], " ")
		}
		simple := m[5]
		if simple == "ARGUMENTS" || simple == "@" {
			return all
		}
		i, _ := strconv.Atoi(simple)
		if i >= 1 && i <= len(args) {
			return args[i-1]
		}
		return ""
	})
}

// ExpandTemplate expands "/name args" if name matches a template.
func ExpandTemplate(text string, templates []Template) (string, bool) {
	if !strings.HasPrefix(text, "/") {
		return text, false
	}
	body := strings.TrimSpace(text[1:])
	name, rest, _ := strings.Cut(body, " ")
	for _, t := range templates {
		if t.Name == name {
			return SubstituteArgs(t.Content, ParseCommandArgs(rest)), true
		}
	}
	return text, false
}
