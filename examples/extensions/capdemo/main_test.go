package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCapdemoBuilds(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "capdemo")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
}
