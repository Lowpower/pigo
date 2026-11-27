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
	if got := Normalize("+"); got != "+" {
		t.Fatalf("+ got %q", got)
	}
	if got := Normalize("ctrl++"); got != "ctrl++" {
		t.Fatalf("ctrl++ got %q", got)
	}
	if got := Normalize("shift+ctrl++"); got != "ctrl+shift++" {
		t.Fatalf("shift+ctrl++ got %q", got)
	}
	if got := Normalize("ctrl+shift+="); got != "ctrl+shift+=" {
		t.Fatalf("physical spelling got %q", got)
	}
	if got := Normalize("ctrl+escape"); got != "ctrl+esc" {
		t.Fatalf("ctrl+escape got %q", got)
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

func TestLoadKeybindingsStripsBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(path, []byte("\ufeff"+`{"app.model.select":"ctrl+k"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newManager(dir, false)
	if !m.Matches("ctrl+k", "app.model.select") {
		t.Fatal("BOM keybindings were ignored")
	}
	legacy := "\ufeff" + `{"selectModel":"ctrl+k"}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if !RewriteLegacyFile(path) {
		t.Fatal("BOM legacy file was not rewritten")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(string(b), "\ufeff") || strings.Contains(string(b), `"selectModel"`) {
		t.Fatalf("rewrite kept BOM or legacy name: %s", b)
	}
	if !strings.Contains(string(b), `"app.model.select"`) {
		t.Fatalf("rewrite dropped binding: %s", b)
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

func TestSchemaKeyIsNotABinding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	const schema = "https://raw.githubusercontent.com/Lowpower/pigo/main/docs/schemas/keybindings.schema.json"
	body := `{"$schema":"` + schema + `","selectModel":"ctrl+k"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newManager(dir, false)
	if _, ok := m.user["$schema"]; ok {
		t.Fatal("$schema was stored as an action")
	}
	if !m.Matches("ctrl+k", "app.model.select") {
		t.Fatal("binding next to $schema was ignored")
	}
	if !RewriteLegacyFile(path) {
		t.Fatal("expected legacy rewrite")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), schema) {
		t.Fatalf("rewrite dropped $schema: %s", b)
	}
	if strings.Contains(string(b), "selectModel") {
		t.Fatalf("legacy name kept: %s", b)
	}
	m.Reload()
	if _, ok := m.user["$schema"]; ok {
		t.Fatal("reload stored $schema as an action")
	}
	if !m.Matches("ctrl+k", "app.model.select") {
		t.Fatal("reload lost the migrated binding")
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
	const want = "Paste files on macOS, images, or text from clipboard"
	if unix.Description("app.clipboard.pasteImage") != want || win.Description("app.clipboard.pasteImage") != want {
		t.Fatalf("description = %q", unix.Description("app.clipboard.pasteImage"))
	}
}

func TestShiftedSymbolMatchesBasePlusShift(t *testing.T) {
	if !Equal("ctrl++", "ctrl+shift+=") || !Equal("ctrl+shift+=", "ctrl++") {
		t.Fatal("ctrl++ should match ctrl+shift+=")
	}
	if !Equal("?", "shift+/") || !Equal("ctrl+?", "ctrl+shift+/") {
		t.Fatal("? should match shift+/")
	}
	if !Equal("ctrl+_", "ctrl+shift+-") {
		t.Fatal("ctrl+_ should match ctrl+shift+-")
	}
	if Equal("ctrl+a", "ctrl+shift+a") || Equal("a", "shift+a") || Equal("shift+l", "l") {
		t.Fatal("letters must keep shift")
	}
	if Equal("ctrl+-", "ctrl+_") || Equal("ctrl+-", "ctrl+shift+-") {
		t.Fatal("ctrl+- is the unshifted key")
	}
	if Equal("shift++", "+") || Equal("shift++", "shift+=") {
		t.Fatal("shift on an already shifted symbol is a different key")
	}
	if !Equal("shift+tab", "shift+tab") || Equal("tab", "shift+tab") {
		t.Fatal("shift+tab stays a distinct binding")
	}
	if !Equal("ctrl+shift+A", "ctrl+shift+a") {
		t.Fatal("letter case still folds")
	}
	shifted := map[string]string{
		"~": "`", "!": "1", "@": "2", "#": "3", "$": "4", "%": "5", "^": "6",
		"&": "7", "*": "8", "(": "9", ")": "0", "_": "-", "+": "=", "|": `\`,
		"{": "[", "}": "]", ":": ";", "<": ",", ">": ".", "?": "/",
	}
	for symbol, base := range shifted {
		if !Equal(symbol, "shift+"+base) {
			t.Fatalf("%s should match shift+%s", symbol, base)
		}
		if Equal(base, "shift+"+base) {
			t.Fatalf("base %s should not match shift+%s", base, base)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"app.model.select":"ctrl++"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newManager(dir, false)
	if !m.Matches("ctrl+shift+=", "app.model.select") {
		t.Fatal("binding ctrl++ should fire on ctrl+shift+=")
	}
	if !m.Matches("ctrl++", "app.model.select") {
		t.Fatal("binding ctrl++ should fire on ctrl++")
	}
	if m.Matches("ctrl+shift+a", "app.clear") || m.Matches("ctrl+shift+a", "app.model.select") {
		t.Fatal("ctrl+a must not match ctrl+shift+a")
	}
	unix := newManager("", false)
	if !unix.Matches("ctrl+a", "tui.editor.cursorLineStart") || unix.Matches("ctrl+shift+a", "tui.editor.cursorLineStart") {
		t.Fatal("ctrl+a must not match ctrl+shift+a")
	}
	if !unix.Matches("ctrl+-", "tui.editor.undo") {
		t.Fatal("undo stays ctrl+-")
	}
	if unix.Matches("ctrl+_", "tui.editor.undo") || unix.Matches("ctrl+shift+-", "tui.editor.undo") {
		t.Fatal("undo must not take the shifted minus key")
	}
	if !unix.Matches("shift+tab", "app.thinking.cycle") || unix.Matches("tab", "app.thinking.cycle") {
		t.Fatal("shift+tab binding changed")
	}
}

func TestShiftedSymbolExtensionAndReserved(t *testing.T) {
	m := newManager("", false)
	if !m.BindExtension("ctrl++", "plus") {
		t.Fatal("ctrl++ should bind")
	}
	if _, ok := m.ExtensionKey("ctrl+shift+="); !ok {
		t.Fatal("extension ctrl++ should match ctrl+shift+=")
	}
	if _, ok := m.ExtensionKey("ctrl++"); !ok {
		t.Fatal("extension ctrl++ should match itself")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"app.clear":"ctrl+shift+="}`), 0o644); err != nil {
		t.Fatal(err)
	}
	reserved := newManager(dir, false)
	if reserved.BindExtension("ctrl++", "nope") {
		t.Fatal("ctrl++ conflicts with reserved ctrl+shift+=")
	}
}

func TestBindExtensionSkipsReservedAndOverrides(t *testing.T) {
	m := newManager("", false)
	if m.BindExtension("ctrl+c", "nope") {
		t.Fatal("reserved ctrl+c must not bind")
	}
	if !m.BindExtension("ctrl+shift+u", "ext") {
		t.Fatal("free key should bind")
	}
	if _, ok := m.ExtensionKey("ctrl+shift+u"); !ok {
		t.Fatal("missing extension key")
	}
	if !strings.Contains(m.HotkeysText(), "ext") {
		t.Fatal("hotkeys should list extension shortcut")
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
