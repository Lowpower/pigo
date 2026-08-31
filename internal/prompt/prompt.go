package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/skills"
)

// Options controls system prompt construction.
type Options struct {
	Cwd              string
	AgentDir         string
	Custom           string
	Append           []string
	NoContextFiles   bool
	ProjectTrusted   bool
	Skills           []skills.Skill
	Tools            []ai.Tool
	IncludeToolHints bool
}

// Build returns the system prompt.
func Build(opts Options) string {
	var b strings.Builder
	if opts.Custom != "" {
		b.WriteString(opts.Custom)
	} else {
		b.WriteString("You are an expert coding assistant in pigo.\n")
		b.WriteString("Be concise. Prefer editing existing files over writing new ones. Use tools to inspect the repo before proposing changes.\n")
		if opts.IncludeToolHints && len(opts.Tools) > 0 {
			b.WriteString("\nAvailable tools:\n")
			for _, t := range opts.Tools {
				b.WriteString("- ")
				b.WriteString(t.Name)
				b.WriteString(": ")
				b.WriteString(t.Description)
				b.WriteByte('\n')
			}
		}
	}
	for _, a := range opts.Append {
		if strings.TrimSpace(a) == "" {
			continue
		}
		b.WriteString("\n")
		b.WriteString(a)
	}
	if !opts.NoContextFiles {
		if ctx := loadContextFiles(opts.Cwd, opts.AgentDir, opts.ProjectTrusted); ctx != "" {
			b.WriteString("\n<project_context>\n")
			b.WriteString(ctx)
			b.WriteString("\n</project_context>\n")
		}
	}
	if hasRead(opts.Tools) && len(opts.Skills) > 0 {
		b.WriteString("\n")
		b.WriteString(skills.FormatForPrompt(opts.Skills))
		b.WriteString("\nWhen a skill is relevant, read its file and follow it.\n")
	}
	if opts.Cwd != "" {
		b.WriteString("\nCurrent working directory: ")
		b.WriteString(opts.Cwd)
		b.WriteByte('\n')
	}
	b.WriteString("Current date: ")
	b.WriteString(time.Now().Format("2006-01-02"))
	b.WriteByte('\n')
	return b.String()
}

func hasRead(tools []ai.Tool) bool {
	for _, t := range tools {
		if t.Name == "read" {
			return true
		}
	}
	return false
}

func loadContextFiles(cwd, agentDir string, trusted bool) string {
	var chunks []string
	seen := map[string]bool{}
	add := func(text string) {
		if text == "" {
			return
		}
		chunks = append(chunks, text)
	}
	if agentDir != "" {
		add(loadDirContext(agentDir, false, seen))
	}
	if cwd == "" {
		return strings.Join(chunks, "\n\n")
	}
	dir := cwd
	for i := 0; i < 12; i++ {
		add(loadDirContext(dir, trusted, seen))
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		dir = parent
	}
	return strings.Join(chunks, "\n\n")
}

func loadDirContext(dir string, includeDotPigo bool, seen map[string]bool) string {
	var parts []string
	if p := firstExisting(dir, "AGENTS.override.md"); p != "" {
		if text := readContextFile(p, seen); text != "" {
			parts = append(parts, text)
		}
	} else {
		if p := firstExisting(dir, "AGENTS.md", "AGENTS.MD"); p != "" {
			if text := readContextFile(p, seen); text != "" {
				parts = append(parts, text)
			}
		}
		if p := firstExisting(dir, "CLAUDE.md", "CLAUDE.MD"); p != "" {
			if text := readContextFile(p, seen); text != "" {
				parts = append(parts, text)
			}
		}
	}
	if includeDotPigo {
		p := filepath.Join(dir, ".pigo", "AGENTS.md")
		if text := readContextFile(p, seen); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func firstExisting(dir string, names ...string) string {
	for _, name := range names {
		p := filepath.Join(dir, name)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func readContextFile(path string, seen map[string]bool) string {
	if seen[path] {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	seen[path] = true
	return "# " + path + "\n" + string(b)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
