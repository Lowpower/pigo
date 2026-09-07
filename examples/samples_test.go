package examples_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/skills"
	"github.com/Lowpower/pigo/internal/theme"
)

func examplesRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}

func TestSampleSkill(t *testing.T) {
	dir := filepath.Join(examplesRoot(t), "skills", "review")
	sk, err := skills.Discover("", "", []string{dir}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(sk) != 1 || sk[0].Name != "review" || sk[0].Description == "" {
		t.Fatalf("skill = %+v", sk)
	}
}

func TestSamplePrompt(t *testing.T) {
	path := filepath.Join(examplesRoot(t), "prompts", "review.md")
	tpls := prompt.DiscoverTemplates("", "", []string{path}, false, false)
	if len(tpls) != 1 || tpls[0].Name != "review" {
		t.Fatalf("templates = %+v", tpls)
	}
	out := prompt.SubstituteArgs(tpls[0].Content, []string{"README.md"})
	if out == "" {
		t.Fatal("empty substitution")
	}
}

func TestSampleTheme(t *testing.T) {
	path := filepath.Join(examplesRoot(t), "themes", "high-contrast.json")
	th := theme.LoadWith(theme.LoadOptions{Name: "high-contrast", Extra: []string{path}, NoDiscovery: true})
	if th.Name != "high-contrast" || th.User == "" {
		t.Fatalf("theme = %+v", th)
	}
}
