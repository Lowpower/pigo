package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandShortFlags(t *testing.T) {
	got := expandShortFlags([]string{"-nt", "-ns", "-nc", "-p", "hi", "--", "-nt"})
	want := []string{"--no-tools", "--no-skills", "--no-context-files", "-p", "hi", "--", "-nt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSplitPromptArgsAtFiles(t *testing.T) {
	msgs, files := splitPromptArgs([]string{"@notes.md", "what is this", "@more.txt"})
	if len(files) != 2 || files[0] != "notes.md" || files[1] != "more.txt" {
		t.Fatalf("files=%v", files)
	}
	if len(msgs) != 1 || msgs[0] != "what is this" {
		t.Fatalf("msgs=%v", msgs)
	}
}

func TestInlineFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := inlineFiles(dir, []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<file name=") || !strings.Contains(got, "hello") {
		t.Fatalf("%s", got)
	}
}

func TestApproveFlagsParse(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Execute()
	s := out.String()
	if !strings.Contains(s, "--approve") || !strings.Contains(s, "--no-approve") {
		t.Fatalf("help missing approve flags:\n%s", s)
	}
}

func TestTuiModeFlagParse(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Execute()
	s := out.String()
	if !strings.Contains(s, "--tui-mode") {
		t.Fatalf("help missing --tui-mode:\n%s", s)
	}
}

func TestThemeFlagsParse(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Execute()
	s := out.String()
	if !strings.Contains(s, "--theme") || !strings.Contains(s, "--no-themes") || !strings.Contains(s, "--use-theme") {
		t.Fatalf("help missing theme flags:\n%s", s)
	}
}

func TestListModelsFiltersByAuth(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--list-models", "--offline", "--config-dir", t.TempDir()})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "openai/gpt-4o") {
		t.Fatalf("missing openai: %s", s)
	}
	if strings.Contains(s, "anthropic/") {
		t.Fatalf("unauthenticated anthropic leaked: %s", s)
	}
}

func TestListModelsEmptyWithoutAuth(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--list-models", "--offline", "--config-dir", t.TempDir()})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("want empty list, got %q", out.String())
	}
}
