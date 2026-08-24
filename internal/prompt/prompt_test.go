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
