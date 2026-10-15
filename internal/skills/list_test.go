package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListCandidatesFollowsSymlinksAndSkipsBroken(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	target := filepath.Join(elsewhere, "linked", "SKILL.md")
	writeSkillFile(t, target, "target\n")
	if err := os.Symlink(filepath.Join(elsewhere, "linked"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "broken")); err != nil {
		t.Fatal(err)
	}
	writeSkillFile(t, filepath.Join(root, "ok", "SKILL.md"), "ok\n")

	outside := filepath.Join(elsewhere, "one.md")
	writeSkillFile(t, outside, "one\n")
	aliasDir := filepath.Join(root, "alias")
	if err := os.MkdirAll(aliasDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(aliasDir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	writeSkillFile(t, filepath.Join(aliasDir, "Examples", "e.md"), "nested\n")
	if err := os.Symlink(filepath.Join(elsewhere, "linked"), filepath.Join(root, "also")); err != nil {
		t.Fatal(err)
	}

	got := ListCandidates(root, true)
	if !containsPath(got, filepath.Join(root, "linked", "SKILL.md")) && !containsPath(got, filepath.Join(root, "also", "SKILL.md")) {
		t.Fatalf("symlink dir not discovered: %v", got)
	}
	if !containsPath(got, filepath.Join(aliasDir, "SKILL.md")) {
		t.Fatalf("symlink file not discovered: %v", got)
	}
	if !containsPath(got, filepath.Join(root, "ok", "SKILL.md")) {
		t.Fatalf("regular skill missing: %v", got)
	}
	if containsPath(got, filepath.Join(aliasDir, "Examples", "e.md")) {
		t.Fatalf("siblings of a symlinked SKILL.md were scanned: %v", got)
	}
	for _, p := range got {
		if strings.Contains(p, "broken") {
			t.Fatalf("broken symlink was returned: %v", got)
		}
	}
	n := 0
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		if resolved == want {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("canonical path count=%d paths=%v", n, got)
	}
}

func TestListCandidatesHonorsIgnoreFiles(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillFile(t, filepath.Join(root, "kept", "SKILL.md"), "kept\n")
	writeSkillFile(t, filepath.Join(root, "ignored", "SKILL.md"), "ignored\n")
	writeSkillFile(t, filepath.Join(root, "notes.md"), "notes\n")
	writeSkillFile(t, filepath.Join(root, "keep.md"), "keep\n")
	writeSkillFile(t, filepath.Join(root, "nested", "secret", "SKILL.md"), "secret\n")
	writeSkillFile(t, filepath.Join(root, "nested", "visible", "SKILL.md"), "visible\n")
	writeSkillFile(t, filepath.Join(root, ".gitignore"), "ignored/\n*.md\n!keep.md\n!SKILL.md\n")
	writeSkillFile(t, filepath.Join(root, "nested", ".ignore"), "secret/\n")
	writeSkillFile(t, filepath.Join(root, ".fdignore"), "!ignored/\n")

	if err := os.WriteFile(filepath.Join(parent, ".gitignore"), []byte("*\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ListCandidates(root, true)
	if containsPath(got, filepath.Join(root, "notes.md")) {
		t.Fatalf("gitignored markdown loaded: %v", got)
	}
	if !containsPath(got, filepath.Join(root, "keep.md")) {
		t.Fatalf("negated keep.md missing: %v", got)
	}
	if !containsPath(got, filepath.Join(root, "ignored", "SKILL.md")) {
		t.Fatalf(".fdignore should re-include ignored/: %v", got)
	}
	if containsPath(got, filepath.Join(root, "nested", "secret", "SKILL.md")) {
		t.Fatalf("subdirectory .ignore did not apply: %v", got)
	}
	if !containsPath(got, filepath.Join(root, "nested", "visible", "SKILL.md")) {
		t.Fatalf("visible nested skill missing: %v", got)
	}
	if !containsPath(got, filepath.Join(root, "kept", "SKILL.md")) {
		t.Fatalf("parent gitignore should not hide skills: %v", got)
	}
}

func TestListCandidatesStopsAtSkillMarkdown(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "real")
	writeSkillFile(t, filepath.Join(skill, "Examples", "e.md"), "---\nname: example\ndescription: Nested example\n---\n\nbody\n")
	writeSkillFile(t, filepath.Join(skill, "SKILL.md"), "---\nname: real\ndescription: The skill\n---\n\nbody\n")

	got := ListCandidates(root, true)
	if len(got) != 1 || got[0] != filepath.Join(skill, "SKILL.md") {
		t.Fatalf("candidates = %v", got)
	}

	loaded, diags := Discover("", "", []string{root}, false, false)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	if len(loaded) != 1 || loaded[0].Name != "real" {
		t.Fatalf("skills = %+v", loaded)
	}
}

func TestListCandidatesRootMarkdownScope(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "root-file.md"), "root\n")
	writeSkillFile(t, filepath.Join(root, "nested-skill", "SKILL.md"), "nested\n")
	writeSkillFile(t, filepath.Join(root, "child-skill", "child-skill.md"), "child\n")
	writeSkillFile(t, filepath.Join(root, "README.md"), "readme\n")

	allowed := ListCandidates(root, true)
	if !containsPath(allowed, filepath.Join(root, "root-file.md")) || !containsPath(allowed, filepath.Join(root, "README.md")) {
		t.Fatalf("allowed root should keep direct markdown: %v", allowed)
	}
	if !containsPath(allowed, filepath.Join(root, "nested-skill", "SKILL.md")) {
		t.Fatalf("missing nested SKILL.md: %v", allowed)
	}
	if containsPath(allowed, filepath.Join(root, "child-skill", "child-skill.md")) {
		t.Fatalf("subdirectory markdown is not a skill: %v", allowed)
	}

	agents := filepath.Join(t.TempDir(), ".agents", "skills")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillFile(t, filepath.Join(agents, "root-file.md"), "Just a readme, no frontmatter.\n")
	writeSkillFile(t, filepath.Join(agents, "nested-skill", "SKILL.md"), "---\nname: nested\ndescription: Nested skill\n---\n\nbody\n")
	writeSkillFile(t, filepath.Join(agents, "child-skill", "child-skill.md"), "---\nname: child\ndescription: Should not load\n---\n\nbody\n")

	denied := ListCandidates(agents, false)
	if containsPath(denied, filepath.Join(agents, "root-file.md")) {
		t.Fatalf(".agents root markdown should be ignored: %v", denied)
	}
	if containsPath(denied, filepath.Join(agents, "child-skill", "child-skill.md")) {
		t.Fatalf(".agents nested markdown should be ignored: %v", denied)
	}
	if !containsPath(denied, filepath.Join(agents, "nested-skill", "SKILL.md")) {
		t.Fatalf("missing .agents SKILL.md: %v", denied)
	}

	loaded, diags := Discover("", "", []string{agents}, false, false)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	if len(loaded) != 1 || loaded[0].Name != "nested" {
		t.Fatalf("skills = %+v", loaded)
	}
}

func TestListCandidatesSymlinkCycle(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "ok", "SKILL.md"), "ok\n")
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	got := ListCandidates(root, true)
	if len(got) != 1 || got[0] != filepath.Join(root, "ok", "SKILL.md") {
		t.Fatalf("candidates = %v", got)
	}
}

func TestListCandidatesSkipsHiddenAndNodeModules(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, ".hidden", "SKILL.md"), "hidden\n")
	writeSkillFile(t, filepath.Join(root, "node_modules", "pkg", "SKILL.md"), "pkg\n")
	writeSkillFile(t, filepath.Join(root, "ok", "SKILL.md"), "ok\n")
	got := ListCandidates(root, true)
	if len(got) != 1 || got[0] != filepath.Join(root, "ok", "SKILL.md") {
		t.Fatalf("candidates = %v", got)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}
