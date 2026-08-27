package shell

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestGetConfigUnixBash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	cfg, err := GetConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Stdin || len(cfg.Args) != 1 || cfg.Args[0] != "-c" {
		t.Fatalf("unix bash = %+v", cfg)
	}
	if cfg.Shell != "/bin/bash" && !strings.Contains(cfg.Shell, "bash") {
		t.Fatalf("shell = %s", cfg.Shell)
	}
}

func TestGetConfigCustomMissing(t *testing.T) {
	_, err := GetConfigPath(filepath.Join(t.TempDir(), "no-such-bash"))
	if err == nil {
		t.Fatal("expected missing custom path error")
	}
}

func TestGetConfigCustomPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bash")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := GetConfigPath(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Shell != p || cfg.Stdin || cfg.Args[0] != "-c" {
		t.Fatalf("%+v", cfg)
	}
}

func TestWindowsGitBashAndLegacyWSL(t *testing.T) {
	origGOOS, origStat, origLook, origEnv := goos, stat, lookPath, getenv
	t.Cleanup(func() {
		goos, stat, lookPath, getenv = origGOOS, origStat, origLook, origEnv
	})
	goos = "windows"
	git := `C:\Program Files\Git\bin\bash.exe`
	getenv = func(k string) string {
		if k == "ProgramFiles" {
			return `C:\Program Files`
		}
		return ""
	}
	stat = func(name string) (os.FileInfo, error) {
		if name == git {
			return fakeFileInfo{}, nil
		}
		return nil, os.ErrNotExist
	}
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	cfg, err := GetConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Shell != git || cfg.Stdin {
		t.Fatalf("git bash = %+v", cfg)
	}

	legacy := `C:\Windows\System32\bash.exe`
	cfg = bashConfig(legacy)
	if !cfg.Stdin || cfg.Args[0] != "-s" {
		t.Fatalf("legacy wsl = %+v", cfg)
	}
}

func TestPowerShellUnixError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	if _, err := GetPowerShellConfig(); err == nil {
		t.Fatal("powershell should be windows-only")
	}
}

func TestPowerShellWindowsResolve(t *testing.T) {
	origGOOS, origStat, origLook := goos, stat, lookPath
	t.Cleanup(func() {
		goos, stat, lookPath = origGOOS, origStat, origLook
	})
	goos = "windows"
	pwsh := `C:\Program Files\PowerShell\7\pwsh.exe`
	lookPath = func(name string) (string, error) {
		if name == "pwsh.exe" {
			return pwsh, nil
		}
		return "", os.ErrNotExist
	}
	stat = func(name string) (os.FileInfo, error) {
		if name == pwsh {
			return fakeFileInfo{}, nil
		}
		return nil, os.ErrNotExist
	}
	cfg, err := GetPowerShellConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Shell != pwsh || strings.Join(cfg.Args, " ") != strings.Join(powershellArgs(), " ") {
		t.Fatalf("%+v", cfg)
	}
}

func TestNormalizeWindowsShellPath(t *testing.T) {
	cases := map[string]string{
		"/c/Users/example/project": `C:\Users\example\project`,
		"/cygdrive/d/work":         `D:\work`,
		"/mnt/e/source":            `E:\source`,
		"/c":                       `C:\`,
		`C:\already`:               `C:\already`,
		"/notadrive/foo":           "/notadrive/foo",
	}
	for in, want := range cases {
		if got := NormalizeWindowsShellPath(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestIsLegacyWSLBash(t *testing.T) {
	if !IsLegacyWSLBash(`C:\Windows\System32\bash.exe`) {
		t.Fatal("system32")
	}
	if !IsLegacyWSLBash(`C:/Windows/Sysnative/bash.exe`) {
		t.Fatal("sysnative")
	}
	if IsLegacyWSLBash(`C:\Program Files\Git\bin\bash.exe`) {
		t.Fatal("git bash is not legacy wsl")
	}
}

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "bash.exe" }
func (fakeFileInfo) Size() int64        { return 1 }
func (fakeFileInfo) Mode() os.FileMode  { return 0o755 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }
