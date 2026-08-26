package pkgmgr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
)

func TestIsEnabledByOverrides(t *testing.T) {
	base := "/tmp/agent"
	path := filepath.Join(base, "extensions", "tool")
	if !IsEnabledByOverrides(path, nil, base) {
		t.Fatal("default enabled")
	}
	if IsEnabledByOverrides(path, []string{"-extensions/tool"}, base) {
		t.Fatal("exact disable")
	}
	if IsEnabledByOverrides(path, []string{"+extensions/tool", "-extensions/tool"}, base) {
		t.Fatal("force-exclude after force-include")
	}
	if IsEnabledByOverrides(path, []string{"-extensions/tool", "+extensions/tool"}, base) {
		t.Fatal("force-exclude still wins when both + and - match")
	}
	if IsEnabledByOverrides(path, []string{"!extensions/*"}, base) {
		t.Fatal("glob exclude")
	}
	if !IsEnabledByOverrides(path, []string{"!extensions/*", "+extensions/tool"}, base) {
		t.Fatal("+exact overrides !glob")
	}
}

func TestToggleGlobalDisablePersists(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	extDir := filepath.Join(agent, "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(extDir, "tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := Open(cwd, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var tool Resource
	for _, r := range rs {
		if r.Path == bin && r.Type == KindExtensions {
			tool = r
		}
	}
	if tool.Path == "" {
		t.Fatalf("missing tool: %+v", rs)
	}
	if err := m.ToggleResource(tool, false, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(agent)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range cfg.Extensions {
		if p == "-extensions/tool" {
			found = true
		}
	}
	if !found {
		t.Fatalf("extensions=%v", cfg.Extensions)
	}
	rs, err = m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	argv := SpawnArgv(rs)
	for _, a := range argv {
		if len(a) > 0 && a[0] == bin {
			t.Fatalf("disabled tool still spawned: %v", argv)
		}
	}
}

func TestToggleProjectCyclePersists(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	extDir := filepath.Join(agent, "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(extDir, "tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := Open(cwd, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, _ := m.Resolve(context.Background())
	var tool Resource
	for _, r := range rs {
		if r.Path == bin {
			tool = r
		}
	}
	if tool.Path == "" {
		t.Fatal("missing tool")
	}
	if st := m.ProjectOverrideState(tool); st != OverrideInherit {
		t.Fatalf("state=%s", st)
	}
	if err := m.ToggleResource(tool, true, true); err != nil {
		t.Fatal(err)
	}
	if st := m.ProjectOverrideState(tool); st != OverrideUnload {
		t.Fatalf("after first cycle state=%s paths=%v", st, m.Project.Extensions)
	}
	if err := m.ToggleResource(tool, true, true); err != nil {
		t.Fatal(err)
	}
	if st := m.ProjectOverrideState(tool); st != OverrideLoad {
		t.Fatalf("after second cycle state=%s", st)
	}
	proj := filepath.Join(cwd, ".pigo", "settings.json")
	b, err := os.ReadFile(proj)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"extensions"`) {
		t.Fatalf("project settings=%s", b)
	}
}

func TestResolveSkillsPromptsThemes(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	skillDir := filepath.Join(agent, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	promptDir := filepath.Join(agent, "prompts")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "hello.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	themeDir := filepath.Join(agent, "themes")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "dark.json"), []byte(`{"name":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Open(cwd, agent, false)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, r := range rs {
		kinds = append(kinds, r.Type+":"+filepath.Base(r.Path))
		if !r.Enabled {
			t.Fatalf("expected enabled: %+v", r)
		}
	}
	joined := strings.Join(kinds, " ")
	if !strings.Contains(joined, "skills:SKILL.md") || !strings.Contains(joined, "prompts:hello.md") || !strings.Contains(joined, "themes:dark.json") {
		t.Fatalf("kinds=%v", kinds)
	}
}
