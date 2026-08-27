package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMovesSessionsCommandsToolsAndKeybindings(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	proj := filepath.Join(cwd, ".pi")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	hdr, _ := json.Marshal(map[string]any{
		"type": "session", "version": 3, "id": "abc",
		"timestamp": "2026-01-01T00:00:00.000Z", "cwd": cwd,
	})
	oldSession := filepath.Join(agent, "old.jsonl")
	if err := os.WriteFile(oldSession, append(append(hdr, '\n'), []byte(`{"type":"message","id":"1"}`+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(agent, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "commands", "hi.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "commands", "local.md"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(agent, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "tools", "rg"), []byte("rg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "tools", "custom.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(agent, "keybindings.json"), []byte(`{"selectModel":"ctrl+k"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	res := Run(cwd, agent)
	if len(res.Warnings) == 0 {
		t.Fatal("expected tools/ custom-file warning")
	}

	if _, err := os.Stat(oldSession); !os.IsNotExist(err) {
		t.Fatalf("old session still present: %v", err)
	}
	moved, err := os.ReadDir(filepath.Join(agent, "sessions"))
	if err != nil || len(moved) == 0 {
		t.Fatalf("sessions dir: %v %v", err, moved)
	}
	if _, err := os.Stat(filepath.Join(agent, "prompts", "hi.md")); err != nil {
		t.Fatalf("global prompts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "prompts", "local.md")); err != nil {
		t.Fatalf("project prompts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agent, "bin", "rg")); err != nil {
		t.Fatalf("bin/rg: %v", err)
	}
	kb, err := os.ReadFile(filepath.Join(agent, "keybindings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kb), "app.model.select") {
		t.Fatalf("keybindings: %s", kb)
	}
}
