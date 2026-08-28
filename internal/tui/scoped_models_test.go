package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/runtime"
)

func TestSlashScopedModelsOpensPicker(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.scopedModelsActive() {
		t.Fatal("expected scoped-models picker")
	}
	view := m.View()
	if !strings.Contains(view, "Model Configuration") {
		t.Fatalf("view =\n%s", view)
	}
}

func TestSlashScopedModelsPreservesExplicitEmptySelection(t *testing.T) {
	cfg := testCfg()
	cfg.EnabledModels = []string{}
	m := New(cfg)
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.scoped.enabled.all {
		t.Fatal("explicit empty enabledModels opened in all mode")
	}
	if len(m.scoped.enabled.ids) != 0 {
		t.Fatalf("enabled ids = %v", m.scoped.enabled.ids)
	}
}

func TestSlashScopedModelsIncludesUnavailableConfiguredIDs(t *testing.T) {
	cfg := testCfg()
	cfg.EnabledModels = []string{"anthropic/claude-sonnet-4", "missing/model"}
	m := New(cfg)
	m.engine = &runtime.Engine{
		Scoped: []models.Spec{{Model: models.Model{Provider: "anthropic", ID: "claude-sonnet-4"}}},
		Opts:   runtime.Options{Config: cfg, Offline: true},
	}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})

	found := false
	for _, id := range m.scoped.enabled.ids {
		if id == "missing/model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unavailable configured id missing from picker: %v", m.scoped.enabled.ids)
	}
	view := m.View()
	if !strings.Contains(view, "missing/model") || !strings.Contains(view, "[unavailable]") {
		t.Fatalf("expected unavailable row in view:\n%s", view)
	}
	if strings.Contains(view, "missing/model ✓") {
		t.Fatalf("unavailable row must show ✗, not ✓:\n%s", view)
	}
	if !strings.Contains(view, "missing/model ✗") {
		t.Fatalf("unavailable row missing ✗:\n%s", view)
	}
}

func TestSlashScopedModelsCLIDoesNotMixSettingsUnmatched(t *testing.T) {
	cfg := testCfg()
	cfg.EnabledModels = []string{"missing/settings"}
	m := New(cfg)
	m.engine = &runtime.Engine{
		Scoped: []models.Spec{{Model: models.Model{Provider: "openai", ID: "gpt-4o"}}},
		Opts: runtime.Options{
			Config:  cfg,
			Models:  []string{"openai/gpt-4o"},
			Offline: true,
		},
	}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	for _, id := range m.scoped.enabled.ids {
		if id == "missing/settings" {
			t.Fatalf("CLI --models mixed settings unmatched: %v", m.scoped.enabled.ids)
		}
	}
	if len(m.scoped.enabled.ids) != 1 || m.scoped.enabled.ids[0] != "openai/gpt-4o" {
		t.Fatalf("CLI scope = %v", m.scoped.enabled.ids)
	}
}

func TestSlashScopedModelsCLIKeepsItsOwnUnmatched(t *testing.T) {
	cfg := testCfg()
	cfg.EnabledModels = []string{"missing/settings"}
	m := New(cfg)
	m.engine = &runtime.Engine{
		Scoped: []models.Spec{{Model: models.Model{Provider: "openai", ID: "gpt-4o"}}},
		Opts: runtime.Options{
			Config:  cfg,
			Models:  []string{"openai/gpt-4o", "missing/cli"},
			Offline: true,
		},
	}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	got := map[string]bool{}
	for _, id := range m.scoped.enabled.ids {
		got[id] = true
	}
	if !got["openai/gpt-4o"] || !got["missing/cli"] {
		t.Fatalf("CLI unmatched dropped: %v", m.scoped.enabled.ids)
	}
	if got["missing/settings"] {
		t.Fatalf("settings unmatched leaked into CLI scope: %v", m.scoped.enabled.ids)
	}
}

func TestSlashScopedModelsCLIEmptyScopeDoesNotFallBackToSettings(t *testing.T) {
	cfg := testCfg()
	cfg.EnabledModels = []string{"anthropic/claude-sonnet-4"}
	m := New(cfg)
	m.engine = &runtime.Engine{
		Opts: runtime.Options{
			Config:  cfg,
			Models:  []string{"missing/cli"},
			Offline: true,
		},
	}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.scoped.enabled.all {
		t.Fatal("unmatched CLI --models fell back to implicit-all")
	}
	if len(m.scoped.enabled.ids) != 1 || m.scoped.enabled.ids[0] != "missing/cli" {
		t.Fatalf("CLI-only unmatched = %v", m.scoped.enabled.ids)
	}
}

func TestScopedRefreshUsesCLIPatternsWhenOpenedEmpty(t *testing.T) {
	cfg := testCfg()
	cfg.EnabledModels = []string{"anthropic/claude-sonnet-4"}
	m := New(cfg)
	m.engine = &runtime.Engine{
		Opts: runtime.Options{
			Config:  cfg,
			Models:  []string{"missing/cli"},
			Offline: true,
		},
	}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	next, _ := m.Update(scopedRefreshMsg{gen: m.scoped.gen})
	m = next.(Model)
	if len(m.scoped.enabled.ids) != 1 || m.scoped.enabled.ids[0] != "missing/cli" {
		t.Fatalf("refresh applied settings instead of CLI: %v", m.scoped.enabled.ids)
	}
}

func TestSlashScopedModelsStartsRefresh(t *testing.T) {
	t.Setenv("PIGO_OFFLINE", "")
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg, CatalogBaseURL: "http://example"}}
	m.editor.SetValue("/scoped-models")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("expected catalog refresh command")
	}
	if m.scoped.refreshStatus != "Refreshing model catalogs…" {
		t.Fatalf("refresh status = %q", m.scoped.refreshStatus)
	}
	if m.scoped.cancel == nil {
		t.Fatal("expected refresh cancel function")
	}
	m.scoped.cancel()
}

func TestScopedModelsEnterDoesNotWriteSettings(t *testing.T) {
	dir := t.TempDir()
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Config: m.cfg}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter}) // toggle current
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil {
		t.Fatal("toggle must not write settings.json")
	}
	if m.engine == nil || len(m.engine.Scoped) == 0 {
		t.Fatal("toggle should set a singleton scoped list from all")
	}
	if !m.scopedModelsActive() {
		t.Fatal("enter must not close the picker")
	}
}

func TestScopedModelsEscKeepsSessionDoesNotSave(t *testing.T) {
	dir := t.TempDir()
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Config: m.cfg}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	scoped := append([]models.Spec(nil), m.engine.Scoped...)
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.scopedModelsActive() {
		t.Fatal("esc closes")
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil {
		t.Fatal("esc must not write settings")
	}
	if len(m.engine.Scoped) != len(scoped) {
		t.Fatalf("esc rolled back scoped: %+v vs %+v", m.engine.Scoped, scoped)
	}
}

func TestScopedModelsCtrlSWritesSettings(t *testing.T) {
	dir := t.TempDir()
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Config: m.cfg}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.scopedModelsActive() {
		t.Fatal("ctrl+s must keep picker open")
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.EnabledModels) == 0 {
		t.Fatalf("expected ids, got %v", loaded.EnabledModels)
	}
	found := false
	for _, e := range m.transcript {
		if strings.Contains(e.rendered, "Model selection saved to settings") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing save status")
	}
	for _, id := range loaded.EnabledModels {
		if !strings.Contains(id, "/") || strings.Contains(id, "*") {
			t.Fatalf("Ctrl+S should write provider/id, got %q", id)
		}
	}
}

func TestScopedModelsCtrlSAllEnabledDeletesKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"theme":"default","enabledModels":["openai/gpt-4o"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Config: m.cfg}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.scoped.enabled.all {
		t.Fatal("expected implicit-all picker")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EnabledModels != nil {
		t.Fatalf("all-enabled should delete the key, got %#v", loaded.EnabledModels)
	}
}

func TestScopedModelsCtrlSKeepsUnavailableIDs(t *testing.T) {
	dir := t.TempDir()
	cfg := testCfg()
	cfg.EnabledModels = []string{"anthropic/claude-sonnet-4", "missing/model"}
	m := New(cfg)
	m.engine = &runtime.Engine{
		Scoped: []models.Spec{{Model: models.Model{Provider: "anthropic", ID: "claude-sonnet-4"}}},
		Opts:   runtime.Options{AgentDir: dir, Config: cfg},
	}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EnabledModels == nil {
		t.Fatal("available-plus-unavailable must not delete enabledModels")
	}
	found := false
	for _, id := range loaded.EnabledModels {
		if id == "missing/model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unavailable id dropped on save: %v", loaded.EnabledModels)
	}
}

func TestModelPickerSeesUpdatedScoped(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	if m.models.scope != scopeScoped {
		t.Fatalf("scope = %s", m.models.scope)
	}
}

func TestScopedRefreshTimeoutCopy(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg, CatalogBaseURL: "http://127.0.0.1:1"}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.scoped.refreshStatus = "Refreshing model catalogs…"
	next, _ := m.Update(scopedRefreshMsg{gen: m.scoped.gen, timedOut: true, failed: []string{"anthropic"}})
	m = next.(Model)
	if !strings.Contains(m.scoped.refreshStatus, "Model refresh timed out; showing cached models.") {
		t.Fatalf("status = %q", m.scoped.refreshStatus)
	}
	if strings.Contains(m.scoped.refreshStatus, "Could not refresh") {
		t.Fatal("timeout must win over failed providers")
	}
}

func TestScopedRefreshStatusIsMuted(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg, Offline: true}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.scoped.refreshStatus = "Refreshing model catalogs…"
	plain := "Refreshing model catalogs…"
	want := m.footerStyle.Render(plain)
	if want == plain {
		t.Fatal("expected footerStyle to emit ANSI in this test")
	}
	view := m.View()
	if !strings.Contains(view, want) {
		t.Fatalf("refresh status not muted:\n%s", view)
	}
}

func TestScopedRefreshDoesNotClobberDirty(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg, CatalogBaseURL: "http://example"}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	before := m.scoped.enabled.clone()
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.scoped.dirty {
		t.Fatal("expected dirty after toggle")
	}
	after := m.scoped.enabled.clone()
	next, _ := m.Update(scopedRefreshMsg{gen: m.scoped.gen})
	m = next.(Model)
	if m.scoped.enabled.all != after.all || len(m.scoped.enabled.ids) != len(after.ids) {
		t.Fatalf("clobbered %+v -> %+v (started %+v)", after, m.scoped.enabled, before)
	}
}

func TestScopedRefreshSkippedOffline(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg, Offline: true, CatalogBaseURL: "http://x"}}
	m.editor.SetValue("/scoped-models")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("offline must not start refresh")
	}
	if strings.Contains(m.View(), "Refreshing model catalogs…") {
		t.Fatal("no refreshing status offline")
	}
}

func TestScopedRefreshDropsPriorGenerationAfterReopen(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg, CatalogBaseURL: "http://example"}}
	first, _ := m.openScopedModels()
	m = first.(Model)
	oldGen := m.scoped.gen
	m.closeScopedModels()
	second, _ := m.openScopedModels()
	m = second.(Model)
	if m.scoped.gen == oldGen {
		t.Fatalf("reused refresh generation %d", oldGen)
	}
	before := m.scoped.refreshStatus
	next, _ := m.Update(scopedRefreshMsg{gen: oldGen, failed: []string{"stale"}})
	m = next.(Model)
	if m.scoped.refreshStatus != before {
		t.Fatalf("stale refresh changed status from %q to %q", before, m.scoped.refreshStatus)
	}
}

func openScopedPicker(t *testing.T) Model {
	t.Helper()
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg, Offline: true}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.scopedModelsActive() {
		t.Fatal("expected scoped-models picker")
	}
	return m
}

func TestScopedModelsCtrlAEnablesAll(t *testing.T) {
	m := openScopedPicker(t)
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.scoped.enabled.all {
		t.Fatal("toggle from all should become a singleton")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	if !m.scoped.enabled.all {
		t.Fatalf("ctrl+a should restore all, got %+v", m.scoped.enabled)
	}
	if !m.scoped.dirty {
		t.Fatal("ctrl+a should mark dirty")
	}
	if len(m.engine.Scoped) != 0 {
		t.Fatalf("implicit-all should clear Engine.Scoped, got %+v", m.engine.Scoped)
	}
}

func TestScopedModelsCtrlXClearsAll(t *testing.T) {
	m := openScopedPicker(t)
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.scoped.enabled.all || len(m.scoped.enabled.ids) != 0 {
		t.Fatalf("ctrl+x should clear checks, got %+v", m.scoped.enabled)
	}
	if !m.scopedModelsActive() {
		t.Fatal("ctrl+x must not close the picker")
	}
	if !strings.Contains(m.View(), "✗") {
		t.Fatalf("cleared picker should show ✗\n%s", m.View())
	}
}

func TestScopedModelsCtrlPTogglesCurrentProvider(t *testing.T) {
	m := openScopedPicker(t)
	first, ok := m.scoped.current()
	if !ok {
		t.Fatal("no selected row")
	}
	prov, _, ok := strings.Cut(first.ID, "/")
	if !ok {
		t.Fatalf("id = %s", first.ID)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.scoped.enabled.all {
		t.Fatal("ctrl+p from all should drop the current provider")
	}
	for _, id := range m.scoped.enabled.ids {
		if strings.HasPrefix(id, prov+"/") {
			t.Fatalf("provider %s still enabled: %v", prov, m.scoped.enabled.ids)
		}
	}
	if len(m.scoped.enabled.ids) == 0 {
		t.Fatal("other providers should stay enabled")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	if !m.scoped.enabled.all {
		t.Fatalf("second ctrl+p should restore all, got %+v", m.scoped.enabled)
	}
}

func TestScopedModelsAltArrowsReorderSession(t *testing.T) {
	m := openScopedPicker(t)
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	first := m.engine.Scoped[0].Provider + "/" + m.engine.Scoped[0].ID
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.engine.Scoped) != 2 {
		t.Fatalf("want two scoped models, got %+v", m.engine.Scoped)
	}
	second := m.engine.Scoped[1].Provider + "/" + m.engine.Scoped[1].ID
	if first == second {
		t.Fatal("expected two distinct models")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	got := []string{
		m.engine.Scoped[0].Provider + "/" + m.engine.Scoped[0].ID,
		m.engine.Scoped[1].Provider + "/" + m.engine.Scoped[1].ID,
	}
	want := []string{second, first}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("alt+up order = %v, want %v", got, want)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	got = []string{
		m.engine.Scoped[0].Provider + "/" + m.engine.Scoped[0].ID,
		m.engine.Scoped[1].Provider + "/" + m.engine.Scoped[1].ID,
	}
	if got[0] != first || got[1] != second {
		t.Fatalf("alt+down should restore original order, got %v", got)
	}
}
