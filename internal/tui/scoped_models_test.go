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
