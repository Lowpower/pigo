package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
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
	got := DiscoverTemplates(cwd, agent, nil, true)
	if len(got) != 1 || got[0].Name != "review" {
		t.Fatalf("%+v", got)
	}
	expanded, ok := ExpandTemplate("/review src/", got)
	if !ok || !strings.Contains(expanded, "Review src/") {
		t.Fatalf("expanded=%q ok=%v", expanded, ok)
	}
}
