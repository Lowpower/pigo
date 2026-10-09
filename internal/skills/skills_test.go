package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverSkipsProjectWhenUntrusted(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo", "skills", "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: projskill\ndescription: Project only\n---\n\nDo not load when untrusted.\n"
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "skills", "proj", "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	got, diags := Discover(cwd, agent, nil, true, false)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	if len(got) != 0 {
		t.Fatalf("untrusted should skip project skills, got %+v", got)
	}
	got, diags = Discover(cwd, agent, nil, true, true)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	if len(got) != 1 || got[0].Name != "projskill" {
		t.Fatalf("trusted should load project skill: %+v", got)
	}
}

func TestDiscoverAndFormat(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	dir := filepath.Join(agent, "skills", "summarize")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: summarize\ndescription: Summarize a file\n---\n\nUse the read tool then summarize.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	got, diags := Discover(cwd, agent, nil, true, true)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	if len(got) != 1 || got[0].Name != "summarize" {
		t.Fatalf("skills = %+v", got)
	}
	xml := FormatForPrompt(got)
	if !strings.Contains(xml, `name="summarize"`) || !strings.Contains(xml, "Summarize a file") {
		t.Fatalf("xml = %s", xml)
	}
	body, ok := ExpandCommand(got, "summarize", "README.md")
	if !ok || !strings.Contains(body, "read tool") || !strings.Contains(body, "README.md") {
		t.Fatalf("expand = %q ok=%v", body, ok)
	}
}

func TestDiscoverWarnsMissingDescription(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	path := filepath.Join(agent, "skills", "blank", "SKILL.md")
	writeSkillFile(t, path, "---\nname: blank\n---\n\nNo description.\n")
	got, diags := Discover(cwd, agent, nil, true, true)
	if len(got) != 0 {
		t.Fatalf("skill without description loaded: %+v", got)
	}
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "has no description") || !strings.Contains(diags[0].Message, path) {
		t.Fatalf("diagnostics = %+v", diags)
	}
}

func TestDiscoverUnclosedFrontmatterIsMissingDescription(t *testing.T) {
	agent := t.TempDir()
	path := filepath.Join(agent, "skills", "open", "SKILL.md")
	writeSkillFile(t, path, "---\nname: open\ndescription: never closed\n\nbody\n")
	got, diags := Discover("", agent, nil, true, false)
	if len(got) != 0 {
		t.Fatalf("unclosed frontmatter loaded: %+v", got)
	}
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "has no description") {
		t.Fatalf("diagnostics = %+v", diags)
	}
}

func TestDiscoverWarnsInvalidNameButLoads(t *testing.T) {
	agent := t.TempDir()
	path := filepath.Join(agent, "skills", "bad", "SKILL.md")
	writeSkillFile(t, path, "---\nname: bad_name\ndescription: still useful\n---\n\nbody\n")
	got, diags := Discover("", agent, nil, true, false)
	if len(got) != 1 || got[0].Name != "bad_name" || got[0].Description != "still useful" {
		t.Fatalf("skills = %+v", got)
	}
	if len(diags) != 1 || !strings.Contains(diags[0].Message, `invalid name "bad_name"`) || !strings.Contains(diags[0].Message, path) {
		t.Fatalf("diagnostics = %+v", diags)
	}
}

func TestDiscoverWarnsLongDescriptionButLoads(t *testing.T) {
	agent := t.TempDir()
	okPath := filepath.Join(agent, "skills", "short", "SKILL.md")
	longPath := filepath.Join(agent, "skills", "long", "SKILL.md")
	writeSkillFile(t, okPath, "---\nname: short\ndescription: "+strings.Repeat("你", 1024)+"\n---\n\nbody\n")
	writeSkillFile(t, longPath, "---\nname: long\ndescription: "+strings.Repeat("你", 1025)+"\n---\n\nbody\n")
	got, diags := Discover("", agent, nil, true, false)
	if len(got) != 2 {
		t.Fatalf("skills = %+v", got)
	}
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "longer than 1024 characters") || !strings.Contains(diags[0].Message, longPath) {
		t.Fatalf("diagnostics = %+v", diags)
	}
	if strings.Contains(diags[0].Message, okPath) {
		t.Fatalf("1024-character description should not warn: %+v", diags)
	}
}

func TestDiscoverRejectsMalformedFrontmatter(t *testing.T) {
	agent := t.TempDir()
	path := filepath.Join(agent, "skills", "broken", "SKILL.md")
	writeSkillFile(t, path, "---\nname: broken\ndescription: Broken: unquoted colon\n---\n\nbody\n")
	plain := filepath.Join(agent, "skills", "notes.md")
	writeSkillFile(t, plain, "---\nname: notes\ndescription: Broken: unquoted colon\n---\n\nbody\n")
	got, diags := Discover("", agent, nil, true, false)
	if len(got) != 0 {
		t.Fatalf("malformed skills loaded: %+v", got)
	}
	if len(diags) != 1 || !strings.HasPrefix(diags[0].Message, "Warning: malformed skill frontmatter in "+path+": ") {
		t.Fatalf("diagnostics = %+v", diags)
	}
}

func TestDiscoverProjectSkillWinsNameCollision(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	user := filepath.Join(agent, "skills", "demo", "SKILL.md")
	proj := filepath.Join(cwd, ".pigo", "skills", "demo", "SKILL.md")
	writeSkillFile(t, user, "---\nname: demo\ndescription: User copy\n---\n\nuser\n")
	writeSkillFile(t, proj, "---\nname: demo\ndescription: Project copy\n---\n\nproject\n")
	got, diags := Discover(cwd, agent, nil, true, true)
	if len(got) != 1 || got[0].Source != "project" || got[0].Description != "Project copy" || got[0].FilePath != proj {
		t.Fatalf("skills = %+v", got)
	}
	if len(diags) != 1 || !strings.Contains(diags[0].Message, user) || !strings.Contains(diags[0].Message, proj) {
		t.Fatalf("diagnostics = %+v", diags)
	}
}

func TestDiscoverKeepsFirstInSameLevel(t *testing.T) {
	agent := t.TempDir()
	first := filepath.Join(agent, "skills", "aaa", "SKILL.md")
	second := filepath.Join(agent, "skills", "zzz", "SKILL.md")
	writeSkillFile(t, first, "---\nname: shared\ndescription: First copy\n---\n\nfirst\n")
	writeSkillFile(t, second, "---\nname: shared\ndescription: Second copy\n---\n\nsecond\n")
	got, diags := Discover("", agent, nil, true, false)
	if len(got) != 1 || got[0].FilePath != first || got[0].Description != "First copy" {
		t.Fatalf("skills = %+v", got)
	}
	if len(diags) != 1 || !strings.Contains(diags[0].Message, first) || !strings.Contains(diags[0].Message, second) {
		t.Fatalf("diagnostics = %+v", diags)
	}
}

func TestDiscoverSymlinkAliasIsNotCollision(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real", "SKILL.md")
	writeSkillFile(t, target, "---\nname: linked\ndescription: One file\n---\n\nbody\n")
	alias := filepath.Join(root, "alias", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	got, diags := Discover("", "", []string{target, alias}, false, false)
	if len(got) != 1 || got[0].Name != "linked" {
		t.Fatalf("skills = %+v", got)
	}
	if len(diags) != 0 {
		t.Fatalf("symlink alias should not warn: %+v", diags)
	}
}

func TestDiscoverNameMismatchIsSilent(t *testing.T) {
	agent := t.TempDir()
	path := filepath.Join(agent, "skills", "other", "SKILL.md")
	writeSkillFile(t, path, "---\nname: review\ndescription: Reviews code\n---\n\nbody\n")
	got, diags := Discover("", agent, nil, true, false)
	if len(got) != 1 || got[0].Name != "review" {
		t.Fatalf("skills = %+v", got)
	}
	if len(diags) != 0 {
		t.Fatalf("name/directory mismatch should not warn: %+v", diags)
	}
}

func TestParseFrontmatter(t *testing.T) {
	folded := "---\nname: review\ndescription: >\n  Review a change\n  for tests.\n---\n\nDo the review.\n"
	literal := "---\nname: review\ndescription: |\n  line one\n  line two\n---\n\nbody\n"
	noNL := "---\nname: demo\ndescription: hi\n---"
	bom := "\uFEFF---\nname: demo\ndescription: hi\n---\n\nbody\n"
	quoted := "---\ndescription: \"hello: world\"\ndisable-model-invocation: true\n---\n\nbody\n"
	trueSpelling := "---\ndisable-model-invocation: True\ndescription: still on\n---\n\nbody\n"
	structured := "---\nname: demo\ndescription: uses structure\nallowed-tools:\n  - read\n  - bash\nmetadata:\n  author: ada\n  license: mit\n---\n\nbody\n"
	dashedLine := "---\ndescription: see\n--- below\n---\nbody\n"
	embedded := "---\ndescription: |\n  keep\n  ---\n  this\n---\nafter\n"
	crlf := "---\r\nname: demo\r\ndescription: hi\r\n---\r\n\r\nbody\r\n"
	emptyFM := "---\n---\nbody\n"
	plain := "Just a body.\n"
	unclosed := "---\nname: open\ndescription: never closed\n\nbody\n"
	syntax := "---\nname: broken\ndescription: Broken: unquoted colon\n---\n\nbody\n"
	seq := "---\n- item\n---\nbody\n"

	tests := []struct {
		name    string
		in      string
		want    map[string]string
		body    string
		wantErr bool
	}{
		{
			name: "folded block",
			in:   folded,
			want: map[string]string{"name": "review", "description": "Review a change for tests.\n"},
			body: "Do the review.",
		},
		{
			name: "literal block",
			in:   literal,
			want: map[string]string{"name": "review", "description": "line one\nline two\n"},
			body: "body",
		},
		{
			name: "closing fence without trailing newline",
			in:   noNL,
			want: map[string]string{"name": "demo", "description": "hi"},
		},
		{
			name: "bom",
			in:   bom,
			want: map[string]string{"name": "demo", "description": "hi"},
			body: "body",
		},
		{
			name: "quotes and bool",
			in:   quoted,
			want: map[string]string{"description": "hello: world", "disable-model-invocation": "true"},
			body: "body",
		},
		{
			name: "bool keeps source spelling",
			in:   trueSpelling,
			want: map[string]string{"disable-model-invocation": "True", "description": "still on"},
			body: "body",
		},
		{
			name: "list and nested map",
			in:   structured,
			want: map[string]string{
				"name":          "demo",
				"description":   "uses structure",
				"allowed-tools": "- read\n- bash",
				"metadata":      "author: ada\nlicense: mit",
			},
			body: "body",
		},
		{
			name: "dashed line is not a fence",
			in:   dashedLine,
			want: map[string]string{"description": "see"},
			body: "body",
		},
		{
			name: "literal keeps dashed line",
			in:   embedded,
			want: map[string]string{"description": "keep\n---\nthis\n"},
			body: "after",
		},
		{
			name: "crlf",
			in:   crlf,
			want: map[string]string{"name": "demo", "description": "hi"},
			body: "body",
		},
		{
			name: "empty frontmatter",
			in:   emptyFM,
			want: map[string]string{},
			body: "body",
		},
		{
			name: "no frontmatter",
			in:   plain,
			want: map[string]string{},
			body: plain,
		},
		{
			name: "unclosed fence",
			in:   unclosed,
			want: map[string]string{},
			body: unclosed,
		},
		{name: "syntax error", in: syntax, wantErr: true},
		{name: "root sequence", in: seq, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := ParseFrontmatter(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if body != tt.body {
				t.Fatalf("body = %q want %q", body, tt.body)
			}
			if len(fm) != len(tt.want) {
				t.Fatalf("frontmatter = %#v want %#v", fm, tt.want)
			}
			for k, v := range tt.want {
				if fm[k] != v {
					t.Errorf("%s = %q want %q", k, fm[k], v)
				}
			}
		})
	}
}

func TestDiscoverFoldedDescriptionAndDisableFlag(t *testing.T) {
	agent := t.TempDir()
	path := filepath.Join(agent, "skills", "review", "SKILL.md")
	writeSkillFile(t, path, "---\nname: review\ndescription: >\n  Review a change\n  for tests.\ndisable-model-invocation: true\n---\n\nbody\n")
	got, diags := Discover("", agent, nil, true, false)
	if len(diags) != 0 || len(got) != 1 {
		t.Fatalf("skills = %+v diagnostics = %+v", got, diags)
	}
	if got[0].Description != "Review a change for tests." || !got[0].DisableLLM {
		t.Fatalf("skill = %+v", got[0])
	}
	if strings.Contains(FormatForPrompt(got), "review") {
		t.Fatal("disabled skill was included in the prompt catalog")
	}
}

func TestDiscoverMalformedFrontmatterLeavesOtherSkills(t *testing.T) {
	agent := t.TempDir()
	bad := filepath.Join(agent, "skills", "broken", "SKILL.md")
	good := filepath.Join(agent, "skills", "ok", "SKILL.md")
	writeSkillFile(t, bad, "---\nname: broken\ndescription: Broken: unquoted colon\n---\n\nbody\n")
	writeSkillFile(t, good, "---\nname: ok\ndescription: Still loads\n---\n\nbody\n")
	got, diags := Discover("", agent, nil, true, false)
	if len(got) != 1 || got[0].Name != "ok" || got[0].Description != "Still loads" {
		t.Fatalf("skills = %+v", got)
	}
	if len(diags) != 1 || !strings.HasPrefix(diags[0].Message, "Warning: malformed skill frontmatter in "+bad+": ") {
		t.Fatalf("diagnostics = %+v", diags)
	}
}

func TestDiscoverPlainMarkdownWithoutDescriptionIsSilent(t *testing.T) {
	agent := t.TempDir()
	writeSkillFile(t, filepath.Join(agent, "skills", "notes.md"), "Just notes, no frontmatter.\n")
	got, diags := Discover("", agent, nil, true, false)
	if len(got) != 0 || len(diags) != 0 {
		t.Fatalf("skills = %+v diagnostics = %+v", got, diags)
	}
}

func writeSkillFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
