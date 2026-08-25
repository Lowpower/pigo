package pkgmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectAutoExtensionEntries(t *testing.T) {
	dir := t.TempDir()
	ext := filepath.Join(dir, "extensions")
	if err := os.MkdirAll(filepath.Join(ext, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "plain.js"), []byte("export default 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "nested", "index.ts"), []byte("export default 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ext, "too", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "too", "deep", "index.js"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := collectAutoExtensionEntries(ext)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "plain.js") {
		t.Fatalf("missing plain.js: %v", got)
	}
	if !strings.Contains(joined, filepath.Join("nested", "index.ts")) && !containsBase(got, "index.ts") {
		t.Fatalf("missing nested/index.ts: %v", got)
	}
	for _, p := range got {
		if strings.Contains(p, filepath.Join("too", "deep")) {
			t.Fatalf("recursed too deep: %v", got)
		}
	}
}

func TestPiManifestExtensions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"pi":{"extensions":["bin/ext"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	extPath := filepath.Join(dir, "bin", "ext")
	if err := os.WriteFile(extPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := resolveExtensionEntries(dir)
	if len(got) != 1 || got[0] != extPath {
		t.Fatalf("got %v want %s", got, extPath)
	}
}

func TestIsSpawnable(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(dir, "mod.js")
	if err := os.WriteFile(mod, []byte("export default 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "run.py")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env python3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsSpawnable(bin) {
		t.Fatal("executable should be spawnable")
	}
	if IsSpawnable(mod) {
		t.Fatal("plain js module should not be spawnable")
	}
	if !IsSpawnable(script) {
		t.Fatal("shebang script should be spawnable")
	}
}

func containsBase(paths []string, base string) bool {
	for _, p := range paths {
		if filepath.Base(p) == base {
			return true
		}
	}
	return false
}
