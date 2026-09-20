package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoSmokeFixture(t *testing.T) {
	s, err := LoadFile(filepath.Join("..", "..", "evals", "smoke.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "smoke" || s.Prompt == "" || !s.NoTools {
		t.Fatalf("fixture = %+v", s)
	}
	if len(s.Expect.Contains) != 1 || s.Expect.Contains[0] != "Paris" {
		t.Fatalf("expect = %+v", s.Expect)
	}
}

func TestLoadDirReadsJSONScenarios(t *testing.T) {
	got, err := LoadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("scenarios = %d, want 1", len(got))
	}
	s := got[0]
	if s.Name != "smoke" || s.Prompt == "" || !s.NoTools {
		t.Fatalf("scenario = %+v", s)
	}
	if len(s.Expect.Contains) != 1 || s.Expect.Contains[0] != "Paris" {
		t.Fatalf("expect = %+v", s.Expect)
	}
}

func TestLoadDirUsesFilenameWhenNameMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capital.json")
	body := `{"prompt":"hi","expect":{"equals":"ok"}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "capital" {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadDirDocsLiftNameDropsDocsSuffix(t *testing.T) {
	dir := t.TempDir()
	body := `{"prompt":"hi","files":{"AGENTS.md":"tabs"},"expect":{"contains":["tabs"]}}`
	if err := os.WriteFile(filepath.Join(dir, "indent.docs.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "indent" || !got[0].DocsLift {
		t.Fatalf("got %+v", got)
	}
}

func TestStripContextDocsKeepsNonDocFiles(t *testing.T) {
	got := stripContextDocs(map[string]string{
		"AGENTS.md":          "tabs",
		"CLAUDE.md":          "spaces",
		".pigo/AGENTS.md":    "secret",
		"AGENTS.override.md": "override",
		"readme.txt":         "keep",
		"src/main.go":        "package main",
	})
	if len(got) != 2 || got["readme.txt"] != "keep" || got["src/main.go"] != "package main" {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadDirRejectsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("expected error")
	}
}

func TestGradeContainsEqualsRegex(t *testing.T) {
	if err := grade("Paris is the capital", Expect{Contains: []string{"Paris"}}); err != nil {
		t.Fatal(err)
	}
	if err := grade("London", Expect{Contains: []string{"Paris"}}); err == nil {
		t.Fatal("expected contains failure")
	}
	if err := grade("Paris", Expect{Equals: "Paris"}); err != nil {
		t.Fatal(err)
	}
	if err := grade(" Paris\n", Expect{Equals: "Paris"}); err != nil {
		t.Fatal(err)
	}
	if err := grade("Paris", Expect{Regex: `^P\w+$`}); err != nil {
		t.Fatal(err)
	}
	if err := grade("Paris", Expect{Regex: `^London$`}); err == nil {
		t.Fatal("expected regex failure")
	}
}
