package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/skills"
	"github.com/Lowpower/pigo/internal/theme"
)

func writeSkill(t *testing.T, root, name, desc string) string {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeSettings(t *testing.T, dir, raw string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testEngineOpts(agent, cwd string) Options {
	return Options{
		AgentDir:     agent,
		Cwd:          cwd,
		Offline:      true,
		NoTools:      true,
		NoExtensions: true,
		Config:       config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
	}
}

func skillNames(sk []skills.Skill) []string {
	var out []string
	for _, s := range sk {
		out = append(out, s.Name)
	}
	return out
}

func templateNames(ts []prompt.Template) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

func TestNewHonorsDisabledSkillOverride(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	writeSkill(t, agent, "demo", "A demo skill")
	writeSettings(t, agent, `{"skills":["-skills/demo/SKILL.md"]}`)

	e, err := New(context.Background(), testEngineOpts(agent, cwd))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if names := skillNames(e.Skills); len(names) != 0 {
		t.Fatalf("disabled skill still loaded: %v", names)
	}

	writeSettings(t, agent, `{}`)
	e.Reload()
	if names := skillNames(e.Skills); len(names) != 1 || names[0] != "demo" {
		t.Fatalf("reload after enabling: %v", names)
	}
}

func TestNewLoadsPackageSkill(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	pkg := filepath.Join(agent, "mypkg")
	writeSkill(t, pkg, "frompkg", "From a local package")
	writeSettings(t, agent, `{"packages":["./mypkg"]}`)

	e, err := New(context.Background(), testEngineOpts(agent, cwd))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if names := skillNames(e.Skills); len(names) != 1 || names[0] != "frompkg" {
		t.Fatalf("package skill missing: %v", names)
	}
}

func TestNewSkipsUntrustedProjectResources(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	writeSkill(t, agent, "userone", "User skill")
	writeSkill(t, filepath.Join(cwd, ".pigo"), "projone", "Project skill")
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "prompts", "proj.md"), []byte("project prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := testEngineOpts(agent, cwd)
	opts.ProjectTrusted = false
	e, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if names := skillNames(e.Skills); len(names) != 1 || names[0] != "userone" {
		t.Fatalf("untrusted skills = %v", names)
	}
	if names := templateNames(e.Templates); len(names) != 0 {
		t.Fatalf("untrusted prompts = %v", names)
	}
}

func TestNoSkillsStillLoadsCLIPath(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	writeSkill(t, agent, "hidden", "Should be skipped by --no-skills")
	extra := t.TempDir()
	path := writeSkill(t, extra, "clipath", "CLI extra skill")

	opts := testEngineOpts(agent, cwd)
	opts.NoSkills = true
	opts.SkillPaths = []string{path}
	e, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if names := skillNames(e.Skills); len(names) != 1 || names[0] != "clipath" {
		t.Fatalf("--no-skills should keep CLI extra, got %v", names)
	}
}

func TestUserSkillBeatsPackageSameName(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	userPath := writeSkill(t, agent, "dup", "From user auto")
	pkg := filepath.Join(agent, "pkgdup")
	writeSkill(t, pkg, "dup", "From package")
	writeSettings(t, agent, `{"packages":["./pkgdup"]}`)

	e, err := New(context.Background(), testEngineOpts(agent, cwd))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if len(e.Skills) != 1 || e.Skills[0].Name != "dup" {
		t.Fatalf("skills = %+v", e.Skills)
	}
	if e.Skills[0].FilePath != userPath {
		t.Fatalf("user auto should win, got %s want %s", e.Skills[0].FilePath, userPath)
	}
}

func TestNewHonorsDisabledPromptAndTheme(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(agent, "prompts", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "prompts", "hello.md"), []byte("hello prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "prompts", "nested", "deep.md"), []byte("nested prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agent, "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "themes", "nord.json"), []byte(`{"name":"nord","accent":"33"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, agent, `{"prompts":["-prompts/hello.md"],"themes":["-themes/nord.json"]}`)

	e, err := New(context.Background(), testEngineOpts(agent, cwd))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if names := templateNames(e.Templates); strings.Join(names, ",") != "deep" {
		t.Fatalf("templates = %v, want only nested deep", names)
	}
	for _, p := range e.ThemeFiles {
		if strings.HasSuffix(p, "nord.json") {
			t.Fatalf("disabled theme still in ThemeFiles: %v", e.ThemeFiles)
		}
	}
	opt := theme.LoadOptions{Name: "nord", Extra: e.ThemeFiles, NoDiscovery: true}
	if th := theme.LoadWith(opt); th.Name == "nord" && th.Accent == "33" {
		t.Fatal("disabled nord file should not win over builtin fallback")
	}
	names := theme.NamesWith(theme.LoadOptions{Extra: e.ThemeFiles, NoDiscovery: true})
	for _, n := range names {
		if n == "nord" {
			t.Fatalf("disabled nord listed in NamesWith: %v", names)
		}
	}
}

func TestNoPromptTplsAndNoThemesKeepCLIExtra(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(agent, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "prompts", "hidden.md"), []byte("hidden\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agent, "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "themes", "hidden.json"), []byte(`{"name":"hidden"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cliPrompt := filepath.Join(t.TempDir(), "cli.md")
	if err := os.WriteFile(cliPrompt, []byte("cli prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliTheme := filepath.Join(t.TempDir(), "cli.json")
	if err := os.WriteFile(cliTheme, []byte(`{"name":"clitheme","accent":"99"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := testEngineOpts(agent, cwd)
	opts.NoPromptTpls = true
	opts.PromptPaths = []string{cliPrompt}
	opts.NoThemes = true
	opts.ThemePaths = []string{cliTheme}
	e, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if names := templateNames(e.Templates); len(names) != 1 || names[0] != "cli" {
		t.Fatalf("CLI prompt extra = %v", names)
	}
	if len(e.ThemeFiles) != 0 {
		t.Fatalf("--no-themes should not cache discovered theme files, got %v", e.ThemeFiles)
	}
	th := theme.LoadWith(theme.LoadOptions{Name: "clitheme", Extra: opts.ThemePaths, NoDiscovery: true})
	if th.Name != "clitheme" {
		t.Fatalf("CLI theme extra = %+v", th)
	}
}
