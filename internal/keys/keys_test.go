package keys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeModifiers(t *testing.T) {
	if got := Normalize("shift+ctrl+p"); got != "ctrl+shift+p" {
		t.Fatalf("got %q", got)
	}
	if got := Normalize("Escape"); got != "esc" {
		t.Fatalf("got %q", got)
	}
	if got := Normalize("RETURN"); got != "enter" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultSelectModel(t *testing.T) {
	m := newManager("", false)
	if !m.Matches("ctrl+l", "app.model.select") {
		t.Fatal("ctrl+l should open model selector")
	}
	if !m.Matches("ctrl+shift+p", "app.model.cycleBackward") {
		t.Fatal("ctrl+shift+p should cycle backward")
	}
	if !m.Matches("shift+ctrl+p", "app.model.cycleBackward") {
		t.Fatal("shift+ctrl+p should normalize to the same binding")
	}
}

func TestUserOverrideAndLegacyName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"selectModel":"ctrl+k","app.clear":["ctrl+x"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newManager(dir, false)
	if !m.Matches("ctrl+k", "app.model.select") {
		t.Fatal("legacy selectModel should migrate")
	}
	if m.Matches("ctrl+l", "app.model.select") {
		t.Fatal("user binding replaces the default")
	}
	if !m.Matches("ctrl+x", "app.clear") {
		t.Fatal("array override")
	}
	if err := os.WriteFile(path, []byte(`{"app.model.select":"ctrl+m"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	if !m.Matches("ctrl+m", "app.model.select") {
		t.Fatal("reload should pick up the new file")
	}
}

func TestHotkeysTextUsesEffectiveKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keybindings.json"), []byte(`{"app.model.select":"ctrl+k"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	text := newManager(dir, false).HotkeysText()
	if !containsStr(text, "ctrl+k") || !containsStr(text, "open model selector") {
		t.Fatalf("hotkeys missing override:\n%s", text)
	}
	if containsStr(text, "ctrl+l") {
		t.Fatalf("default ctrl+l should be replaced:\n%s", text)
	}
}

func TestRewriteLegacyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"selectModel":"ctrl+k","app.clear":"ctrl+x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !RewriteLegacyFile(path) {
		t.Fatal("expected rewrite")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, `"app.model.select"`) || strings.Contains(got, `"selectModel"`) {
		t.Fatalf("not migrated: %s", got)
	}
	if RewriteLegacyFile(path) {
		t.Fatal("second rewrite should be a no-op")
	}
}

func TestWindowsCycleBackward(t *testing.T) {
	m := newManager("", true)
	if !m.Matches("alt+p", "app.model.cycleBackward") {
		t.Fatal("windows/WSL cycle backward is alt+p")
	}
	if m.Matches("ctrl+shift+p", "app.model.cycleBackward") {
		t.Fatal("windows should not keep shift+ctrl+p")
	}
}

func TestPasteImageDefaultKeys(t *testing.T) {
	unix := newManager("", false)
	if !unix.Matches("ctrl+v", "app.clipboard.pasteImage") {
		t.Fatal("unix paste image is ctrl+v")
	}
	win := newManager("", true)
	if !win.Matches("alt+v", "app.clipboard.pasteImage") {
		t.Fatal("windows paste image is alt+v")
	}
	if win.Matches("ctrl+v", "app.clipboard.pasteImage") {
		t.Fatal("windows should not keep ctrl+v for paste image")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
