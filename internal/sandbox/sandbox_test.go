package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadMissingIsDisabled(t *testing.T) {
	cfg := Load(t.TempDir(), t.TempDir())
	if Active(cfg) {
		t.Fatal("missing config should not wrap bash")
	}
}

func TestLoadProjectEnables(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "sandbox.json"), []byte(`{"enabled": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load(cwd, t.TempDir())
	if runtime.GOOS == "windows" {
		if Active(cfg) {
			t.Fatal("windows must not wrap")
		}
		return
	}
	if !Active(cfg) {
		t.Fatal("project enabled:true should wrap")
	}
}

func TestProjectOverridesGlobal(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(agent, "extensions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "extensions", "sandbox.json"), []byte(`{"enabled":true,"filesystem":{"allowWrite":["/tmp"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "sandbox.json"), []byte(`{"enabled":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load(cwd, agent)
	if Active(cfg) {
		t.Fatal("project enabled:false should win")
	}
}

func TestNoSandboxFlagDisables(t *testing.T) {
	SetNoSandbox(true)
	t.Cleanup(func() { SetNoSandbox(false) })
	on := true
	if Active(Config{Enabled: &on}) {
		t.Fatal("--no-sandbox should disable wrapping")
	}
}

func TestCommandUnchangedWhenDisabled(t *testing.T) {
	SetNoSandbox(false)
	name, args := Command("echo hi", t.TempDir(), t.TempDir())
	if name != "bash" || strings.Join(args, " ") != "-c echo hi" {
		t.Fatalf("got %s %v", name, args)
	}
}

func TestWrapArgvIncludesBwrapAndAllowWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no bwrap wrap on windows")
	}
	on := true
	cwd := t.TempDir()
	argv := WrapArgv("echo hi", cwd, Config{
		Enabled:    &on,
		Filesystem: Filesystem{AllowWrite: []string{".", "/tmp"}},
		Network:    Network{AllowedDomains: []string{"github.com"}},
	})
	joined := strings.Join(argv, " ")
	if argv[0] != "bwrap" || !strings.Contains(joined, "--ro-bind / /") {
		t.Fatalf("argv=%v", argv)
	}
	if !strings.Contains(joined, "--bind "+cwd+" "+cwd) {
		t.Fatalf("missing cwd bind: %v", argv)
	}
	if !strings.Contains(joined, "-- bash -c echo hi") {
		t.Fatalf("missing bash -c: %v", argv)
	}
	if strings.Contains(joined, "--unshare-net") {
		t.Fatal("allowedDomains should keep network")
	}
}

func TestWrapArgvUnsharesNetWhenNoDomains(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	argv := WrapArgv("true", t.TempDir(), Config{})
	if !strings.Contains(strings.Join(argv, " "), "--unshare-net") {
		t.Fatalf("argv=%v", argv)
	}
}
