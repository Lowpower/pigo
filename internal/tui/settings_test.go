package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/config"
)

func TestSlashSettingsOpensMenu(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsActive() {
		t.Fatal("/settings should open the settings menu")
	}
	view := m.View()
	if !strings.Contains(view, "Auto-compact") || !strings.Contains(view, "Mermaid") {
		t.Fatalf("menu view:\n%s", view)
	}
	if strings.Contains(view, "settings dir:") {
		t.Fatalf("should not print the old path note:\n%s", view)
	}
}

func TestSettingsEnterCyclesAndPersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	m := New(testCfg())
	m.editor.SetValue("/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("mermaid")})
	view := m.View()
	if !strings.Contains(view, "streaming") {
		t.Fatalf("expected current mermaid value:\n%s", view)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cfg.MermaidMode() != "off" {
		t.Fatalf("cycle mermaid = %s", m.cfg.MermaidMode())
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MermaidMode() != "off" {
		t.Fatalf("saved mermaid = %s", loaded.MermaidMode())
	}
}

func TestSettingsEscapeCloses(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsActive() {
		t.Fatal("esc should close settings")
	}
}

func TestSettingsTogglesShowImages(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	m := New(testCfg())
	m.editor.SetValue("/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("show images")})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cfg.ShowImages() {
		t.Fatal("show images should toggle off")
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"showImages": false`) {
		t.Fatalf("saved: %s", b)
	}
}

func TestSettingsTogglesChangelogAndTelemetry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	m := New(testCfg())
	m.editor.SetValue("/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	view := m.View()
	if !strings.Contains(view, "Collapse changelog") {
		t.Fatalf("menu missing collapse changelog:\n%s", view)
	}

	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("collapse changelog")})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.cfg.CollapsedChangelog() {
		t.Fatal("collapse changelog should toggle on")
	}

	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	m.editor.SetValue("/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("install telemetry")})
	if !strings.Contains(m.View(), "Install telemetry") {
		t.Fatalf("menu missing install telemetry:\n%s", m.View())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cfg.InstallTelemetryEnabled() {
		t.Fatal("install telemetry should toggle off")
	}

	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.CollapsedChangelog() {
		t.Fatal("saved collapseChangelog")
	}
	if loaded.InstallTelemetryEnabled() {
		t.Fatal("saved enableInstallTelemetry")
	}
}
