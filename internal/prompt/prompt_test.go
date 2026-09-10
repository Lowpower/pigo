package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/skills"
)

func TestBuildIncludesContextAndCwd(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte("Use tabs."), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Build(Options{
		Cwd:              cwd,
		Tools:            []ai.Tool{{Name: "read", Description: "read a file"}},
		IncludeToolHints: true,
	})
	if !strings.Contains(got, cwd) {
		t.Fatalf("missing cwd in prompt:\n%s", got)
	}
	if !strings.Contains(got, "Use tabs.") {
		t.Fatalf("missing AGENTS.md:\n%s", got)
	}
	if !strings.Contains(got, "read a file") {
		t.Fatalf("missing tool hint:\n%s", got)
	}
}

func TestOverridePreferredOverAgentsAndClaude(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "svc")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root-agents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.md"), []byte("nested-agents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "CLAUDE.md"), []byte("nested-claude"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.override.md"), []byte("nested-override"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Build(Options{Cwd: nested})
	if !strings.Contains(got, "nested-override") {
		t.Fatalf("missing override:\n%s", got)
	}
	if strings.Contains(got, "nested-agents") || strings.Contains(got, "nested-claude") {
		t.Fatalf("override should replace AGENTS.md/CLAUDE.md in the same dir:\n%s", got)
	}
	if !strings.Contains(got, "root-agents") {
		t.Fatalf("ancestor AGENTS.md should still layer:\n%s", got)
	}
}

func TestGlobalAgentDirAndCaseVariants(t *testing.T) {
	cwd := t.TempDir()
	agent := t.TempDir()
	if err := os.WriteFile(filepath.Join(agent, "AGENTS.md"), []byte("global-agents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.MD"), []byte("cwd-caps"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Build(Options{Cwd: cwd, AgentDir: agent})
	if !strings.Contains(got, "global-agents") {
		t.Fatalf("missing global:\n%s", got)
	}
	if !strings.Contains(got, "cwd-caps") {
		t.Fatalf("missing AGENTS.MD:\n%s", got)
	}
}

func TestBuildSkipsUntrustedProjectAgents(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte("root-agents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "AGENTS.md"), []byte("project-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	untrusted := Build(Options{Cwd: cwd})
	if !strings.Contains(untrusted, "root-agents") {
		t.Fatalf("missing root AGENTS.md:\n%s", untrusted)
	}
	if strings.Contains(untrusted, "project-secret") {
		t.Fatalf("untrusted loaded .pigo/AGENTS.md:\n%s", untrusted)
	}
	trusted := Build(Options{Cwd: cwd, ProjectTrusted: true})
	if !strings.Contains(trusted, "project-secret") {
		t.Fatalf("trusted missing .pigo/AGENTS.md:\n%s", trusted)
	}
}

func TestContextFilePathsMatchesLoadedFiles(t *testing.T) {
	cwd := t.TempDir()
	agent := t.TempDir()
	nested := filepath.Join(cwd, "svc")
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(agent, "AGENTS.md")
	root := filepath.Join(cwd, "AGENTS.md")
	override := filepath.Join(nested, "AGENTS.override.md")
	project := filepath.Join(cwd, ".pigo", "AGENTS.md")
	for _, item := range []struct {
		path, body string
	}{
		{global, "global-agents"},
		{root, "root-agents"},
		{override, "nested-override"},
		{project, "project-secret"},
	} {
		if err := os.WriteFile(item.path, []byte(item.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := ContextFilePaths(nested, agent, true)
	want := []string{global, override, root, project}
	if len(got) != len(want) {
		t.Fatalf("paths=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths[%d]=%s want %s (all=%v)", i, got[i], want[i], got)
		}
	}
	untrusted := ContextFilePaths(nested, agent, false)
	for _, p := range untrusted {
		if p == project {
			t.Fatalf("untrusted listed .pigo/AGENTS.md: %v", untrusted)
		}
	}
}

func TestDiscoverTemplates(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	dir := filepath.Join(agent, "prompts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ndescription: Review a path\n---\n\nReview $1\n"
	if err := os.WriteFile(filepath.Join(dir, "review.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := DiscoverTemplates(cwd, agent, nil, true, true)
	if len(got) != 1 || got[0].Name != "review" {
		t.Fatalf("%+v", got)
	}
	expanded, ok := ExpandTemplate("/review src/", got)
	if !ok || !strings.Contains(expanded, "Review src/") {
		t.Fatalf("expanded=%q ok=%v", expanded, ok)
	}

	proj := filepath.Join(cwd, ".pigo", "prompts")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "secret.md"), []byte("---\ndescription: Secret\n---\n\nhidden\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skipped := DiscoverTemplates(cwd, agent, nil, true, false)
	if len(skipped) != 1 || skipped[0].Name != "review" {
		t.Fatalf("untrusted templates = %+v", skipped)
	}
	withProj := DiscoverTemplates(cwd, agent, nil, true, true)
	if len(withProj) != 2 {
		t.Fatalf("trusted templates = %+v", withProj)
	}
}

func TestBuildInjectsSkillsWithBashOnly(t *testing.T) {
	got := Build(Options{
		Skills: []skills.Skill{{Name: "demo", Description: "Do the demo", FilePath: "/tmp/SKILL.md"}},
		Tools:  []ai.Tool{{Name: "bash"}},
	})
	if !strings.Contains(got, "Use bash to load a skill") || !strings.Contains(got, `name="demo"`) {
		t.Fatalf("bash skills missing:\n%s", got)
	}
	none := Build(Options{
		Skills: []skills.Skill{{Name: "demo", Description: "Do the demo", FilePath: "/tmp/SKILL.md"}},
		Tools:  []ai.Tool{{Name: "edit"}},
	})
	if strings.Contains(none, "<available_skills>") {
		t.Fatalf("skills leaked without read/bash:\n%s", none)
	}
}
