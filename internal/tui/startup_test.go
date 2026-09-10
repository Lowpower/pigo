package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/skills"
	"github.com/Lowpower/pigo/internal/version"
)

func TestStartupHeaderCompact(t *testing.T) {
	m := New(testCfg())
	view := m.View()
	if !strings.Contains(view, "pigo") {
		t.Fatalf("missing logo:\n%s", view)
	}
	if !strings.Contains(view, "v"+version.Version) {
		t.Fatalf("missing version:\n%s", view)
	}
	if !strings.Contains(view, "interrupt") {
		t.Fatalf("missing compact hint:\n%s", view)
	}
	if !strings.Contains(view, "full startup help and loaded resources") {
		t.Fatalf("missing expand hint:\n%s", view)
	}
	if !strings.Contains(view, "look up its docs") {
		t.Fatalf("missing onboarding:\n%s", view)
	}
	if !strings.Contains(view, "Ask it how to use or extend pigo") {
		t.Fatalf("missing onboarding ask:\n%s", view)
	}
	if strings.Contains(view, "provider=") {
		t.Fatalf("old provider= header still present:\n%s", view)
	}
	if strings.Contains(view, "to interrupt") {
		t.Fatalf("compact header should not list full help:\n%s", view)
	}
}

func TestQuietStartupHidesHeaderAndResources(t *testing.T) {
	m := New(testCfg())
	on := true
	m.cfg.QuietStartupFlag = &on
	m.engine = &runtime.Engine{
		Skills: []skills.Skill{{Name: "demo", FilePath: "/tmp/skill/SKILL.md"}},
	}
	view := m.View()
	if strings.Contains(view, "look up its docs") {
		t.Fatalf("quiet startup still showed header:\n%s", view)
	}
	if strings.Contains(view, "[Skills]") || strings.Contains(view, "demo") {
		t.Fatalf("quiet startup still showed skills:\n%s", view)
	}
}

func TestStartupResourcesCompactAndExpanded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	agent := filepath.Join(home, ".pigo", "agent")
	if err := os.MkdirAll(agent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(agent, "AGENTS.md")
	local := filepath.Join(cwd, "AGENTS.md")
	if err := os.WriteFile(global, []byte("global"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(home, "skills", "commit", "SKILL.md")
	m := New(testCfg())
	m.engine = &runtime.Engine{
		Opts: runtime.Options{Cwd: cwd, AgentDir: agent, ProjectTrusted: true},
		Skills: []skills.Skill{
			{Name: "zebra", FilePath: skillPath},
			{Name: "commit", FilePath: filepath.Join(home, "skills", "other", "SKILL.md")},
		},
		Templates: []prompt.Template{{Name: "review", FilePath: filepath.Join(home, "prompts", "review.md")}},
		Scoped:    []models.Spec{{Model: models.Model{Provider: "anthropic", ID: "claude-sonnet-4"}, Thinking: "high"}},
	}
	view := m.View()
	if !strings.Contains(view, "[Context]") {
		t.Fatalf("missing context section:\n%s", view)
	}
	if !strings.Contains(view, "~/.pigo/agent/AGENTS.md") {
		t.Fatalf("missing compact global context:\n%s", view)
	}
	if !strings.Contains(view, "AGENTS.md") {
		t.Fatalf("missing local context name:\n%s", view)
	}
	if strings.Contains(view, cwd+string(os.PathSeparator)+"AGENTS.md") {
		t.Fatalf("compact context should use relative name:\n%s", view)
	}
	if !strings.Contains(view, "[Skills]") || !strings.Contains(view, "commit") || !strings.Contains(view, "zebra") {
		t.Fatalf("missing compact skills:\n%s", view)
	}
	if strings.Contains(view, "SKILL.md") {
		t.Fatalf("compact skills should list names, not paths:\n%s", view)
	}
	if !strings.Contains(view, "[Prompts]") || !strings.Contains(view, "review") {
		t.Fatalf("missing compact prompts:\n%s", view)
	}
	if strings.Contains(view, "review.md") {
		t.Fatalf("compact prompts should list names, not paths:\n%s", view)
	}
	if !strings.Contains(view, "Model scope: anthropic/claude-sonnet-4:high") {
		t.Fatalf("missing model scope:\n%s", view)
	}

	m.toolsExpanded = true
	view = m.View()
	if !strings.Contains(view, "to interrupt") {
		t.Fatalf("expanded header missing full help:\n%s", view)
	}
	if strings.Contains(view, "full startup help and loaded resources") {
		t.Fatalf("expanded header still has compact onboarding:\n%s", view)
	}
	if !strings.Contains(view, "SKILL.md") {
		t.Fatalf("expanded skills missing paths:\n%s", view)
	}
	if !strings.Contains(view, "review.md") {
		t.Fatalf("expanded prompts missing paths:\n%s", view)
	}
	if !strings.Contains(view, formatDisplayPath(local, home)) {
		t.Fatalf("expanded context missing local path:\n%s", view)
	}
}

func TestStartupListingFollowsEngineReload(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{
		Skills: []skills.Skill{{Name: "alpha", FilePath: "/tmp/alpha/SKILL.md"}},
	}
	if !strings.Contains(m.View(), "alpha") {
		t.Fatalf("missing initial skill:\n%s", m.View())
	}
	m.engine.Skills = []skills.Skill{{Name: "beta", FilePath: "/tmp/beta/SKILL.md"}}
	view := m.View()
	if strings.Contains(view, "alpha") {
		t.Fatalf("stale skill after reload:\n%s", view)
	}
	if !strings.Contains(view, "beta") {
		t.Fatalf("missing reloaded skill:\n%s", view)
	}
}

func TestClearKeepsStartupHeader(t *testing.T) {
	m := New(testCfg())
	m.transcript = []entry{{role: "meta", rendered: "gone-soon"}}
	m.editor.SetValue("/clear")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	view := m.View()
	if strings.Contains(view, "gone-soon") {
		t.Fatalf("transcript should clear:\n%s", view)
	}
	if !strings.Contains(view, "look up its docs") {
		t.Fatalf("startup header should survive /clear:\n%s", view)
	}
}

func TestCtrlODoesNotNoteToolOutputInTranscript(t *testing.T) {
	m := New(testCfg())
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if !m.toolsExpanded {
		t.Fatal("ctrl+o should expand")
	}
	view := m.View()
	if strings.Contains(view, "tool output:") {
		t.Fatalf("ctrl+o should not append a transcript note:\n%s", view)
	}
	if !strings.Contains(view, "to expand tools") {
		t.Fatalf("ctrl+o should expand startup help:\n%s", view)
	}
}
