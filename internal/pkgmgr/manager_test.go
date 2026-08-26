package pkgmgr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
)

func TestInstallLocalAndList(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	ext := filepath.Join(cwd, "my-ext")
	if err := os.WriteFile(ext, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := Open(cwd, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	if err := m.InstallAndPersist(context.Background(), ext, false); err != nil {
		t.Fatal(err)
	}
	got := m.ListConfigured()
	if len(got) != 1 || got[0].Scope != "user" || got[0].Source != ext {
		t.Fatalf("%+v", got)
	}
	cfg, err := config.Load(agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Source != ext {
		t.Fatalf("settings packages=%+v", cfg.Packages)
	}
}

func TestRemoveMissing(t *testing.T) {
	m, err := Open(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := m.RemoveAndPersist(context.Background(), "npm:@no/such", false)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected not found")
	}
}

func TestResolveAutoAndSpawnable(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	userExt := filepath.Join(agent, "extensions")
	if err := os.MkdirAll(userExt, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(userExt, "tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(userExt, "skip.js")
	if err := os.WriteFile(mod, []byte("export default 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	projExt := filepath.Join(cwd, ".pigo", "extensions")
	if err := os.MkdirAll(projExt, 0o755); err != nil {
		t.Fatal(err)
	}
	projBin := filepath.Join(projExt, "proj")
	if err := os.WriteFile(projBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	trusted, err := Open(cwd, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	trusted.AutoInstall = false
	rs, err := trusted.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	argv := SpawnArgv(rs)
	if len(argv) != 2 {
		t.Fatalf("spawnable=%v resources=%+v", argv, rs)
	}

	untrusted, err := Open(cwd, agent, false)
	if err != nil {
		t.Fatal(err)
	}
	untrusted.AutoInstall = false
	rs, err = untrusted.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	argv = SpawnArgv(rs)
	if len(argv) != 1 || argv[0][0] != bin {
		t.Fatalf("untrusted spawnable=%v", argv)
	}
}

func TestInstallNpmStub(t *testing.T) {
	agent := t.TempDir()
	m, err := Open(t.TempDir(), agent, false)
	if err != nil {
		t.Fatal(err)
	}
	var ran []string
	m.Run = func(_ context.Context, _ string, args []string, _ string) error {
		ran = append(ran, strings.Join(append([]string{"npm"}, args...), " "))
		// pretend npm laid down the package
		p := filepath.Join(m.npmRoot(false), "node_modules", "left-pad")
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(p, "package.json"), []byte(`{"name":"left-pad"}`), 0o644)
	}
	if err := m.InstallAndPersist(context.Background(), "npm:left-pad", false); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || !strings.Contains(ran[0], "install left-pad") {
		t.Fatalf("run=%v", ran)
	}
	if p := m.InstalledPath("npm:left-pad", false); p == "" {
		t.Fatal("missing install path")
	}
	cfg, _ := config.Load(agent)
	if len(cfg.Packages) != 1 || cfg.Packages[0].Source != "npm:left-pad" {
		t.Fatalf("%+v", cfg.Packages)
	}
}

func TestProjectInstallRequiresTrust(t *testing.T) {
	m, err := Open(t.TempDir(), t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	err = m.InstallAndPersist(context.Background(), "./x", true)
	if err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("err=%v", err)
	}
}

func TestUpdateExtensionsStub(t *testing.T) {
	agent := t.TempDir()
	m, err := Open(t.TempDir(), agent, false)
	if err != nil {
		t.Fatal(err)
	}
	m.User.Packages = []config.PackageEntry{config.StringPackage("npm:left-pad")}
	var ran []string
	m.Run = func(_ context.Context, _ string, args []string, _ string) error {
		ran = append(ran, strings.Join(args, " "))
		return nil
	}
	if err := m.Update(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || !strings.Contains(ran[0], "left-pad@latest") {
		t.Fatalf("run=%v", ran)
	}
}
