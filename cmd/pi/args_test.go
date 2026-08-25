package main

import (
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

func TestApplyModelSpec(t *testing.T) {
	var provider, model, thinking string
	applyModelSpec(&provider, &model, &thinking, "openai/gpt-4o:high")
	if provider != "openai" || model != "gpt-4o" || thinking != "high" {
		t.Fatalf("provider=%s model=%s thinking=%s", provider, model, thinking)
	}
}
