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
		if ctx := loadContextFiles(opts.Cwd, opts.ProjectTrusted); ctx != "" {
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

func loadContextFiles(cwd string, trusted bool) string {
	if cwd == "" {
		return ""
	}
	var chunks []string
	dir := cwd
	for i := 0; i < 12; i++ {
		if p := filepath.Join(dir, "AGENTS.override.md"); fileExists(p) {
			if b, err := os.ReadFile(p); err == nil {
				chunks = append(chunks, "# "+p+"\n"+string(b))
			}
		} else {
			for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
				p := filepath.Join(dir, name)
				b, err := os.ReadFile(p)
				if err != nil {
					continue
				}
				chunks = append(chunks, "# "+p+"\n"+string(b))
			}
		}
		if trusted {
			p := filepath.Join(dir, ".pigo", "AGENTS.md")
			if b, err := os.ReadFile(p); err == nil {
				chunks = append(chunks, "# "+p+"\n"+string(b))
			}
		}
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

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
