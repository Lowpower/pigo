package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/pkgmgr"
)

func TestConfigSelectorSpaceAndTab(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	extDir := filepath.Join(agent, "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(extDir, "tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgr, err := pkgmgr.Open(cwd, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	mgr.AutoInstall = false
	m, err := NewConfigSelector(mgr, false)
	if err != nil {
		t.Fatal(err)
	}
	view := m.View()
	if !strings.Contains(view, "Global Resources") || !strings.Contains(view, "[x]") {
		t.Fatalf("view=%s", view)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(*ConfigSelector)
	if !strings.Contains(m.View(), "[ ]") {
		t.Fatalf("after space: %s", m.View())
	}
	cfg, err := os.ReadFile(filepath.Join(agent, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "-extensions/tool") {
		t.Fatalf("settings=%s", cfg)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*ConfigSelector)
	if !strings.Contains(m.View(), "Project Local Resources") {
		t.Fatalf("after tab: %s", m.View())
	}
}
