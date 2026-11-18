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
	m = submit(m, "/settings")
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
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsActive() {
		t.Fatal("esc should close settings")
	}
}

func TestSettingsEnterCyclesAndPersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	m := New(testCfg())
	m = submit(m, "/settings")
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

func TestSettingsCyclesDefaultProjectTrust(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	m := New(testCfg())
	m = submit(m, "/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("default project trust")})
	if !strings.Contains(m.View(), "ask") {
		t.Fatalf("expected default ask:\n%s", m.View())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cfg.ProjectTrustDefault() != "always" {
		t.Fatalf("cycle = %s", m.cfg.ProjectTrustDefault())
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProjectTrustDefault() != "always" {
		t.Fatalf("saved = %s", loaded.ProjectTrustDefault())
	}
}

func TestSettingsTogglesImages(t *testing.T) {
	tests := []struct {
		label, want string
		got         func(Model) bool
		wantVal     bool
	}{
		{"show images", `"showImages": false`, func(m Model) bool { return m.cfg.ShowImages() }, false},
		{"block images", `"blockImages": true`, func(m Model) bool { return m.cfg.BlockImages() }, true},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PIGO_CODING_AGENT_DIR", dir)
			m := New(testCfg())
			m = submit(m, "/settings")
			m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.label)})
			m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
			if tt.got(m) != tt.wantVal {
				t.Fatalf("%s should toggle to %v", tt.label, tt.wantVal)
			}
			b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), tt.want) {
				t.Fatalf("saved: %s", b)
			}
		})
	}
}

func TestSettingsTogglesChangelogAndTelemetry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	m := New(testCfg())
	m = submit(m, "/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("collapse changelog")})
	view := m.View()
	if !strings.Contains(view, "Collapse changelog") {
		t.Fatalf("menu missing collapse changelog:\n%s", view)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.cfg.CollapsedChangelog() {
		t.Fatal("collapse changelog should toggle on")
	}

	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = submit(m, "/settings")
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

func TestSettingsCyclesShareDestination(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	m := New(testCfg())
	m = submit(m, "/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("session share")})
	if !strings.Contains(m.View(), "Session share destination") || !strings.Contains(m.View(), "auto") {
		t.Fatalf("menu:\n%s", m.View())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cfg.ShareDestination() != "gist" {
		t.Fatalf("cycle = %s", m.cfg.ShareDestination())
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ShareDestination() != "gist" {
		t.Fatalf("saved = %s", loaded.ShareDestination())
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"sessionShareDestination": "gist"`) {
		t.Fatalf("saved file: %s", b)
	}
}

func TestSettingsCyclesWheelScrollLines(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	m := New(testCfg())
	if got := strings.Join(m.settingChoices("fullscreen-wheel-scroll-lines"), ","); got != "auto,1,2,3,5,10" {
		t.Fatalf("default choices = %s", got)
	}
	m.cfg.FullscreenWheelScrollLines = 7
	if got := strings.Join(m.settingChoices("fullscreen-wheel-scroll-lines"), ","); got != "auto,1,2,3,5,7,10" {
		t.Fatalf("custom choices = %s", got)
	}
	m = submit(m, "/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("wheel scrolling")})
	if !strings.Contains(m.View(), "Fullscreen wheel scrolling") || !strings.Contains(m.View(), "7") {
		t.Fatalf("menu:\n%s", m.View())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if n, auto := m.cfg.WheelScrollLines(); auto || n != 10 {
		t.Fatalf("cycle = %d auto=%v", n, auto)
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n, auto := loaded.WheelScrollLines(); auto || n != 10 {
		t.Fatalf("saved = %d auto=%v", n, auto)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"fullscreenWheelScrollLines": 10`) {
		t.Fatalf("saved file: %s", b)
	}
}

func TestSettingsCyclesScrollbarAndAnthropicWarning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	m := New(testCfg())
	m = submit(m, "/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fullscreen scrollbar")})
	if !strings.Contains(m.View(), "Fullscreen scrollbar auto") {
		t.Fatalf("expected default auto:\n%s", m.View())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cfg.ScrollbarMode() != "hidden" {
		t.Fatalf("cycle scrollbar = %s", m.cfg.ScrollbarMode())
	}

	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = submit(m, "/settings")
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("anthropic extra-usage")})
	if !strings.Contains(m.View(), "true") {
		t.Fatalf("expected default true:\n%s", m.View())
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cfg.AnthropicExtraUsageWarning() {
		t.Fatal("warning should toggle off")
	}

	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ScrollbarMode() != "hidden" {
		t.Fatalf("saved scrollbar = %s", loaded.ScrollbarMode())
	}
	if loaded.AnthropicExtraUsageWarning() {
		t.Fatal("saved warning should be false")
	}
}
