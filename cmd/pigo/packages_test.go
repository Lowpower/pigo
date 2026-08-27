package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageInstallListRemove(t *testing.T) {
	agent := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", agent)
	cwd := t.TempDir()
	t.Chdir(cwd)
	ext := filepath.Join(cwd, "ext.sh")
	if err := os.WriteFile(ext, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"install", ext})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if !strings.Contains(out.String(), "Installed") {
		t.Fatalf("install: %s", out.String())
	}

	root = newRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if !strings.Contains(out.String(), ext) {
		t.Fatalf("list: %s", out.String())
	}

	root = newRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"uninstall", ext})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
}

func TestUpdateSelfNotImplemented(t *testing.T) {
	t.Setenv("PIGO_CODING_AGENT_DIR", t.TempDir())
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"update"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "self-update") {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Extensions are skipped") {
		t.Fatalf("missing skip note: %s", out.String())
	}

	root = newRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"update", "--models"})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("models err=%v", err)
	}
}

func TestExpandNoApproveAlias(t *testing.T) {
	got := expandShortFlags([]string{"install", "npm:x", "-na", "-l"})
	want := []string{"install", "npm:x", "--no-approve", "-l"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%v", got)
	}
}

func TestConfigPrint(t *testing.T) {
	agent := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", agent)
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"config", "--print"})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "provider=") || !strings.Contains(s, "model=") {
		t.Fatalf("print: %s", s)
	}
}

func TestConfigLocalRequiresTrust(t *testing.T) {
	t.Setenv("PIGO_CODING_AGENT_DIR", t.TempDir())
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo", "extensions"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"config", "-l"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
}
