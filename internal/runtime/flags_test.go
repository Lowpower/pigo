package runtime

import (
	"context"
	"testing"
)

func TestNoBuiltinToolsLeavesRegistryEmptyWithoutExtensions(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Options{
		Cwd:            t.TempDir(),
		AgentDir:       t.TempDir(),
		NoBuiltinTools: true,
		NoExtensions:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if n := len(e.Tools.List()); n != 0 {
		t.Fatalf("tools=%d, want 0 builtins", n)
	}
}

func TestNoToolsSkipsCLIExtensions(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Options{
		Cwd:           t.TempDir(),
		AgentDir:      t.TempDir(),
		NoTools:       true,
		CLIExtensions: []string{"/bin/true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if len(e.Hosts) != 0 {
		t.Fatalf("hosts=%d, --no-tools should not spawn extensions", len(e.Hosts))
	}
	if n := len(e.Tools.List()); n != 0 {
		t.Fatalf("tools=%d", n)
	}
}

func TestDefaultLoadsBuiltinTools(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if n := len(e.Tools.List()); n != 7 {
		t.Fatalf("tools=%d, want 7 builtins", n)
	}
}
