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

func TestHasProjectResourcesSandboxJSON(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "sandbox.json"), []byte(`{"enabled":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasProjectResources(cwd) {
		t.Fatal("expected .pigo/sandbox.json to require trust")
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

func TestHasProjectResourcesAgentsSkills(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "sub", ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasProjectResources(filepath.Join(cwd, "sub")) {
		t.Fatal("expected ancestor .agents/skills to require trust")
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
}

func TestDecideNoResourcesIsTrusted(t *testing.T) {
	if !Decide(nil, t.TempDir(), Options{}) {
		t.Fatal("no project resources should be trusted")
	}
}

func TestDecideAskWithoutStoreIsUntrusted(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if Decide(nil, cwd, Options{Default: "ask"}) {
		t.Fatal("ask with no saved decision should be untrusted")
	}
	if Decide(nil, cwd, Options{Default: "never"}) {
		t.Fatal("never")
	}
	if !Decide(nil, cwd, Options{Default: "always"}) {
		t.Fatal("always")
	}
}

func TestDecideUsesStore(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := Open(agent)
	if err := s.Set(cwd, true); err != nil {
		t.Fatal(err)
	}
	if !Decide(s, cwd, Options{Default: "ask"}) {
		t.Fatal("stored true")
	}
}
