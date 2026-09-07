package pkgmgr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkillMD(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func skillResources(rs []Resource) []Resource {
	var out []Resource
	for _, r := range rs {
		if r.Type == KindSkills {
			out = append(out, r)
		}
	}
	return out
}

func findSkill(rs []Resource, path string) (Resource, bool) {
	for _, r := range rs {
		if r.Type == KindSkills && r.Path == path {
			return r, true
		}
	}
	return Resource{}, false
}

func TestCollectAncestorAgentsSkillDirsStopsAtGitRoot(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	nested := filepath.Join(repo, "mid", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := collectAncestorAgentsSkillDirs(nested)
	want := []string{
		filepath.Join(nested, ".agents", "skills"),
		filepath.Join(repo, "mid", ".agents", "skills"),
		filepath.Join(repo, ".agents", "skills"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestCollectAncestorAgentsSkillDirsWalksPastFixtureWithoutGit(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got := collectAncestorAgentsSkillDirs(nested)
	for _, dir := range []string{
		filepath.Join(nested, ".agents", "skills"),
		filepath.Join(root, "a", ".agents", "skills"),
		filepath.Join(root, ".agents", "skills"),
		filepath.Join(filepath.Dir(root), ".agents", "skills"),
	} {
		found := false
		for _, g := range got {
			if g == dir {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %s in %v", dir, got)
		}
	}
}

func TestCollectSkillEntriesKeepsRootMarkdown(t *testing.T) {
	dir := t.TempDir()
	rootMD := filepath.Join(dir, "root-file.md")
	if err := os.WriteFile(rootMD, []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := writeSkillMD(t, filepath.Join(dir, "nested-skill"), "# nested\n")
	got := collectSkillEntries(dir)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, rootMD) {
		t.Fatalf("skill collection should keep root markdown: %v", got)
	}
	if !strings.Contains(joined, nested) {
		t.Fatalf("missing nested SKILL.md: %v", got)
	}
}

func TestCollectAgentsSkillEntriesIgnoresRootMarkdown(t *testing.T) {
	dir := t.TempDir()
	rootMD := filepath.Join(dir, "root-file.md")
	if err := os.WriteFile(rootMD, []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	nestedSkill := writeSkillMD(t, filepath.Join(dir, "nested-skill"), "# nested\n")
	childDir := filepath.Join(dir, "child-skill")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatal(err)
	}
	childMD := filepath.Join(childDir, "child-skill.md")
	if err := os.WriteFile(childMD, []byte("child"), 0o644); err != nil {
		t.Fatal(err)
	}
	deepDir := filepath.Join(dir, "deep", "inner")
	if err := os.MkdirAll(deepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	deepMD := filepath.Join(deepDir, "deep-skill.md")
	if err := os.WriteFile(deepMD, []byte("deep"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := collectAgentsSkillEntries(dir)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, rootMD) {
		t.Fatalf("agents mode should ignore root markdown: %v", got)
	}
	for _, want := range []string{nestedSkill, childMD, deepMD} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %v", want, got)
		}
	}
}

func TestResolveAgentsSkillsStopsAtGitRoot(t *testing.T) {
	isolateHome(t)
	agent := t.TempDir()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	nested := filepath.Join(repo, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	above := writeSkillMD(t, filepath.Join(root, ".agents", "skills", "above"), "# above\n")
	rootSkill := writeSkillMD(t, filepath.Join(repo, ".agents", "skills", "root-skill"), "# root\n")
	nestedSkill := writeSkillMD(t, filepath.Join(nested, ".agents", "skills", "nested-skill"), "# nested\n")

	m, err := Open(nested, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findSkill(rs, nestedSkill); !ok {
		t.Fatalf("missing nested skill: %+v", skillResources(rs))
	}
	if _, ok := findSkill(rs, rootSkill); !ok {
		t.Fatalf("missing repo-root skill: %+v", skillResources(rs))
	}
	if _, ok := findSkill(rs, above); ok {
		t.Fatalf("skill above git root should be omitted: %+v", skillResources(rs))
	}
	for _, p := range []string{nestedSkill, rootSkill} {
		r, _ := findSkill(rs, p)
		if r.Scope != "project" || r.Source != "auto" || r.Origin != "top-level" {
			t.Fatalf("metadata %+v", r)
		}
		if r.BaseDir != filepath.Dir(filepath.Dir(filepath.Dir(p))) {
			t.Fatalf("baseDir=%q for %s", r.BaseDir, p)
		}
	}
}

func TestResolveAgentsSkillsWalksAncestorsWithoutGit(t *testing.T) {
	isolateHome(t)
	agent := t.TempDir()
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	mid := writeSkillMD(t, filepath.Join(root, "a", ".agents", "skills", "mid"), "# mid\n")
	top := writeSkillMD(t, filepath.Join(root, ".agents", "skills", "top"), "# top\n")

	m, err := Open(nested, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findSkill(rs, mid); !ok {
		t.Fatalf("missing mid ancestor skill: %+v", skillResources(rs))
	}
	if _, ok := findSkill(rs, top); !ok {
		t.Fatalf("missing top ancestor skill: %+v", skillResources(rs))
	}
}

func TestResolveUserAgentsSkillsAlwaysAndStayUserUnderHome(t *testing.T) {
	home := isolateHome(t)
	agent := t.TempDir()
	cwd := filepath.Join(home, "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	userSkill := writeSkillMD(t, filepath.Join(home, ".agents", "skills", "home-skill"), "# home\n")
	userBase := filepath.Join(home, ".agents")

	m, err := Open(cwd, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var matches []Resource
	for _, r := range skillResources(rs) {
		if r.Path == userSkill {
			matches = append(matches, r)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("home skill count=%d resources=%+v", len(matches), skillResources(rs))
	}
	r := matches[0]
	if r.Scope != "user" || r.Source != "auto" || r.Origin != "top-level" {
		t.Fatalf("want user/auto/top-level, got %+v", r)
	}
	if r.BaseDir != userBase {
		t.Fatalf("baseDir=%q want %q", r.BaseDir, userBase)
	}
	if !r.Enabled {
		t.Fatalf("expected enabled: %+v", r)
	}
}

func TestResolveUntrustedSkipsProjectAgentsKeepsUserAgents(t *testing.T) {
	home := isolateHome(t)
	agent := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	projSkill := writeSkillMD(t, filepath.Join(cwd, ".agents", "skills", "proj"), "# proj\n")
	userSkill := writeSkillMD(t, filepath.Join(home, ".agents", "skills", "user"), "# user\n")

	m, err := Open(cwd, agent, false)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findSkill(rs, projSkill); ok {
		t.Fatalf("untrusted should omit project .agents skill: %+v", skillResources(rs))
	}
	r, ok := findSkill(rs, userSkill)
	if !ok {
		t.Fatalf("untrusted should keep user .agents skill: %+v", skillResources(rs))
	}
	if r.Scope != "user" {
		t.Fatalf("scope=%q", r.Scope)
	}

	global, err := m.ResolveGlobal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findSkill(global, projSkill); ok {
		t.Fatalf("ResolveGlobal should omit project .agents skill")
	}
	if _, ok := findSkill(global, userSkill); !ok {
		t.Fatalf("ResolveGlobal should keep user .agents skill")
	}
}

func TestResolveDedupesSymlinkedAgentSkillsToAgents(t *testing.T) {
	home := isolateHome(t)
	cwd := t.TempDir()
	agent := t.TempDir()
	userSkill := writeSkillMD(t, filepath.Join(home, ".agents", "skills", "foo"), "# foo\n")
	if err := os.Symlink(filepath.Join(home, ".agents", "skills"), filepath.Join(agent, "skills")); err != nil {
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
	n := 0
	for _, r := range skillResources(rs) {
		base, err := filepath.EvalSymlinks(r.Path)
		if err != nil {
			base = r.Path
		}
		want, err := filepath.EvalSymlinks(userSkill)
		if err != nil {
			want = userSkill
		}
		if base == want {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("symlink skill count=%d resources=%+v", n, skillResources(rs))
	}
}

func TestResolveCanonicalDedupePrefersProjectAutoOverUserAuto(t *testing.T) {
	isolateHome(t)
	agent := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	shared := t.TempDir()
	skill := writeSkillMD(t, filepath.Join(shared, "foo"), "# foo\n")
	if err := os.MkdirAll(filepath.Join(cwd, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(agent, "skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(cwd, ".agents", "skills")); err != nil {
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
	want, err := filepath.EvalSymlinks(skill)
	if err != nil {
		t.Fatal(err)
	}
	var matches []Resource
	for _, r := range skillResources(rs) {
		got, err := filepath.EvalSymlinks(r.Path)
		if err != nil {
			got = r.Path
		}
		if got == want {
			matches = append(matches, r)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("count=%d resources=%+v", len(matches), skillResources(rs))
	}
	if matches[0].Scope != "project" {
		t.Fatalf("canonical collision should keep project auto, got %+v", matches[0])
	}
}

func TestResolveUserAgentsOverrideRelativeToAgentsBase(t *testing.T) {
	home := isolateHome(t)
	agent := t.TempDir()
	cwd := t.TempDir()
	foo := writeSkillMD(t, filepath.Join(home, ".agents", "skills", "foo"), "# foo\n")
	bar := writeSkillMD(t, filepath.Join(home, ".agents", "skills", "bar"), "# bar\n")

	m, err := Open(cwd, agent, false)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	m.User.Skills = []string{"-skills/foo"}
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := findSkill(rs, foo); !ok || r.Enabled {
		t.Fatalf("foo should be disabled: %+v", r)
	}
	if r, ok := findSkill(rs, bar); !ok || !r.Enabled {
		t.Fatalf("bar should stay enabled: %+v", r)
	}
}

func TestResolveProjectAgentsOverrideRelativeToAgentsBase(t *testing.T) {
	isolateHome(t)
	agent := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	foo := writeSkillMD(t, filepath.Join(cwd, ".agents", "skills", "foo"), "# foo\n")
	bar := writeSkillMD(t, filepath.Join(cwd, ".agents", "skills", "bar"), "# bar\n")

	m, err := Open(cwd, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	m.Project.Skills = []string{"-skills/foo"}
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := findSkill(rs, foo); !ok || r.Enabled {
		t.Fatalf("project foo should be disabled: %+v", r)
	}
	if r, ok := findSkill(rs, bar); !ok || !r.Enabled {
		t.Fatalf("project bar should stay enabled: %+v", r)
	}
	if r, _ := findSkill(rs, foo); r.BaseDir != filepath.Join(cwd, ".agents") {
		t.Fatalf("project baseDir=%q", r.BaseDir)
	}
}

func TestResolveAgentsSkillsIgnoresRootMarkdown(t *testing.T) {
	isolateHome(t)
	agent := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(cwd, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rootMD := filepath.Join(skillsDir, "root-file.md")
	if err := os.WriteFile(rootMD, []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := writeSkillMD(t, filepath.Join(skillsDir, "nested-skill"), "# nested\n")
	childDir := filepath.Join(skillsDir, "child-skill")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatal(err)
	}
	childMD := filepath.Join(childDir, "child-skill.md")
	if err := os.WriteFile(childMD, []byte("child"), 0o644); err != nil {
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
	if _, ok := findSkill(rs, rootMD); ok {
		t.Fatalf("root markdown should not resolve: %+v", skillResources(rs))
	}
	if _, ok := findSkill(rs, nested); !ok {
		t.Fatalf("missing nested SKILL.md: %+v", skillResources(rs))
	}
	if _, ok := findSkill(rs, childMD); !ok {
		t.Fatalf("missing nested markdown skill: %+v", skillResources(rs))
	}
}
