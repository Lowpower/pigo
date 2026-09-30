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

func TestPigoManifestExtensions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"pigo":{"extensions":["bin/ext"]}}`), 0o644); err != nil {
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

func containsBase(paths []string, base string) bool {
	for _, p := range paths {
		if filepath.Base(p) == base {
			return true
		}
	}
	return false
}
