package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalHelp(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"eval", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "eval") || !strings.Contains(s, "--out") {
		t.Fatalf("eval help:\n%s", s)
	}
}

func TestEvalSkipsWithoutAPIKey(t *testing.T) {
	clearCatalogEnvs(t)
	dir := t.TempDir()
	body := `{"prompt":"What is the capital of France?","noTools":true,"expect":{"contains":["Paris"]}}`
	if err := os.WriteFile(filepath.Join(dir, "smoke.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"eval", dir, "--out", outDir, "--provider", "anthropic"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if !strings.Contains(strings.ToLower(out.String()), "skip") {
		t.Fatalf("want skip in:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "report.json")); err != nil {
		t.Fatal(err)
	}
}
