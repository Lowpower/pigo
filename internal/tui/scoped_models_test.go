package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
