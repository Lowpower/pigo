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
	if tool := skillFileReadTool(opts.Tools); tool != "" && len(opts.Skills) > 0 {
		b.WriteString("\n")
		b.WriteString(skills.FormatForPrompt(opts.Skills, tool))
		b.WriteByte('\n')
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

func skillFileReadTool(tools []ai.Tool) string {
	have := map[string]bool{}
	for _, t := range tools {
		have[t.Name] = true
	}
	for _, name := range []string{"read", "bash"} {
		if have[name] {
			return name
		}
	}
	return ""
}

// ContextFilePaths returns the AGENTS.md / CLAUDE.md / override files that
// Build injects, in load order.
func ContextFilePaths(cwd, agentDir string, trusted bool) []string {
	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] || !fileExists(p) {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}
	collectDir := func(dir string, includeDotPigo bool) {
		if p := firstExisting(dir, "AGENTS.override.md"); p != "" {
			add(p)
		} else {
			add(firstExisting(dir, "AGENTS.md", "AGENTS.MD"))
			add(firstExisting(dir, "CLAUDE.md", "CLAUDE.MD"))
		}
		if includeDotPigo {
			add(filepath.Join(dir, ".pigo", "AGENTS.md"))
		}
	}
	if agentDir != "" {
		collectDir(agentDir, false)
	}
	if cwd == "" {
		return paths
	}
	dir := cwd
	for i := 0; i < 12; i++ {
		collectDir(dir, trusted)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		dir = parent
	}
	return paths
}

func loadContextFiles(cwd, agentDir string, trusted bool) string {
	var chunks []string
	for _, p := range ContextFilePaths(cwd, agentDir, trusted) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		chunks = append(chunks, "# "+p+"\n"+string(b))
	}
	return strings.Join(chunks, "\n\n")
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

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
