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
	got, err := Discover(cwd, agent, nil, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("untrusted should skip project skills, got %+v", got)
	}
	got, err = Discover(cwd, agent, nil, true, true)
	if err != nil {
		t.Fatal(err)
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
	got, err := Discover(cwd, agent, nil, true, true)
	if err != nil {
		t.Fatal(err)
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
