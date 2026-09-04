package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/version"
)

func clearCatalogEnvs(t *testing.T) {
	t.Helper()
	for _, id := range models.ProviderIDs() {
		spec, ok := models.LookupProvider(id)
		if !ok {
			continue
		}
		for _, name := range spec.Env {
			t.Setenv(name, "")
		}
	}
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
}

func TestVersionFlag(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	want := "pigo " + version.Version + "\n"
	if out.String() != want {
		t.Fatalf("got %q want %q", out.String(), want)
	}
}

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

func TestNoSandboxFlagParse(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Execute()
	s := out.String()
	if !strings.Contains(s, "--no-sandbox") {
		t.Fatalf("help missing --no-sandbox:\n%s", s)
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
	clearCatalogEnvs(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")
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

func TestListModelsIncludesGroqWhenKeySet(t *testing.T) {
	clearCatalogEnvs(t)
	t.Setenv("GROQ_API_KEY", "g")
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--list-models", "--offline", "--config-dir", t.TempDir()})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "groq/") || !strings.Contains(s, "openai-completions") {
		t.Fatalf("missing groq: %s", s)
	}
	if strings.Contains(s, "openai/gpt-4o") {
		t.Fatalf("unauthenticated openai leaked: %s", s)
	}
}

func TestListModelsEmptyWithoutAuth(t *testing.T) {
	clearCatalogEnvs(t)
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

func TestResolvePromptInputFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "extra.md")
	if err := os.WriteFile(p, []byte("from-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolvePromptInput(p); got != "from-file" {
		t.Fatalf("got %q", got)
	}
	if got := resolvePromptInput("literal text"); got != "literal text" {
		t.Fatalf("literal %q", got)
	}
}

func TestBuildInitialMessageJoinsStdin(t *testing.T) {
	got, rest := buildInitialMessage("stdin-bit", "file-bit", []string{"one", "two"})
	if got != "stdin-bitfile-bitone" {
		t.Fatalf("initial=%q", got)
	}
	if len(rest) != 1 || rest[0] != "two" {
		t.Fatalf("rest=%v", rest)
	}
}

func TestSessionDirAndVerboseFlagsParse(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Execute()
	s := out.String()
	for _, want := range []string{"--session-dir", "--verbose", "PIGO_TELEMETRY", "PIGO_CODING_AGENT_SESSION_DIR"} {
		if !strings.Contains(s, want) {
			t.Fatalf("help missing %s:\n%s", want, s)
		}
	}
	for _, drop := range []string{"PI_EXPERIMENTAL", "PI_TELEMETRY"} {
		if strings.Contains(s, drop) {
			t.Fatalf("help still mentions %s:\n%s", drop, s)
		}
	}
}

func TestResolveSessionDirOrder(t *testing.T) {
	t.Setenv("PIGO_CODING_AGENT_SESSION_DIR", "/from-env")
	if got := resolveSessionDir("/flag", "/from-settings", ""); got != "/flag" {
		t.Fatalf("flag should win, got %q", got)
	}
	if got := resolveSessionDir("", "/from-settings", ""); got != "/from-settings" {
		t.Fatalf("settings=%q", got)
	}
	if got := resolveSessionDir("", "", ""); got != "/from-env" {
		t.Fatalf("env=%q", got)
	}
}
