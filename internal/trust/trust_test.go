package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	s := Open(dir)
	if _, ok := s.Get(cwd); ok {
		t.Fatal("expected no decision")
	}
	if err := s.Set(cwd, true); err != nil {
		t.Fatal(err)
	}
	v, ok := s.Get(cwd)
	if !ok || !v {
		t.Fatalf("got trusted=%v ok=%v", v, ok)
	}
	child := filepath.Join(cwd, "sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	v, ok = s.Get(child)
	if !ok || !v {
		t.Fatalf("child should inherit parent trust, got %v %v", v, ok)
	}
}

func TestHasProjectResources(t *testing.T) {
	cwd := t.TempDir()
	if HasProjectResources(cwd) {
		t.Fatal("empty cwd")
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo", "extensions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasProjectResources(cwd) {
		t.Fatal("expected .pigo/extensions to require trust")
	}
}

func TestResolveOverride(t *testing.T) {
	yes, no := true, false
	if !Resolve(nil, t.TempDir(), &yes) {
		t.Fatal("approve")
	}
	if Resolve(nil, t.TempDir(), &no) {
		t.Fatal("no-approve")
	}
	if Resolve(nil, t.TempDir(), nil) {
		t.Fatal("default untrusted")
	}
}
