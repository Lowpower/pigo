package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyProjectOverlaysTrustedSettings(t *testing.T) {
	user, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if user.BlockImages() {
		t.Fatal("user default blockImages")
	}
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"defaultTools":["grep"],"images":{"blockImages":true},"shellPath":"/bin/custom-bash","theme":"light","defaultProjectTrust":"always"}`
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	untrusted := ApplyProject(user, cwd, false)
	if untrusted.BlockImages() || untrusted.ShellPath != "" {
		t.Fatalf("untrusted applied project settings: %+v", untrusted)
	}
	if strings.Join(untrusted.InitialBuiltinTools(), ",") != "read,bash,edit,write" {
		t.Fatalf("untrusted tools=%v", untrusted.InitialBuiltinTools())
	}

	got := ApplyProject(user, cwd, true)
	if !got.BlockImages() {
		t.Fatal("trusted should overlay blockImages")
	}
	if got.ShellPath != "/bin/custom-bash" {
		t.Fatalf("shellPath=%q", got.ShellPath)
	}
	if strings.Join(got.InitialBuiltinTools(), ",") != "grep" {
		t.Fatalf("tools=%v", got.InitialBuiltinTools())
	}
	if got.Theme != "light" {
		t.Fatalf("theme=%q", got.Theme)
	}
	if got.ProjectTrustDefault() != "ask" {
		t.Fatalf("defaultProjectTrust must stay global, got %q", got.ProjectTrustDefault())
	}
}

func TestApplyProjectEmptyListDisablesTools(t *testing.T) {
	user, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "settings.json"), []byte(`{"defaultTools":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ApplyProject(user, cwd, true)
	if n := len(got.InitialBuiltinTools()); n != 0 {
		t.Fatalf("empty project defaultTools should disable builtins, n=%d", n)
	}
}

func TestCopyUISettingsLeavesDefaultTools(t *testing.T) {
	tools := []string{"grep"}
	dst := Config{DefaultTools: &tools, Theme: "default"}
	src := Config{Theme: "light", DefaultTools: nil}
	on := true
	src.Images.BlockImages = &on
	CopyUISettings(&dst, src)
	if dst.Theme != "light" {
		t.Fatalf("theme=%q", dst.Theme)
	}
	if dst.DefaultTools == nil || strings.Join(*dst.DefaultTools, ",") != "grep" {
		t.Fatalf("defaultTools should stay user-only, got %v", dst.DefaultTools)
	}
	if !dst.BlockImages() {
		t.Fatal("blockImages not copied")
	}
}
