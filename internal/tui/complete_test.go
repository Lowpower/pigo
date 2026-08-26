package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/slash"
)

func TestSlashSuggestionsUniqueHotkeys(t *testing.T) {
	items, prefix, ok := slashSuggestions("/hotk", slash.Builtins())
	if !ok || prefix != "/hotk" || len(items) != 1 || items[0].Value != "hotkeys" {
		t.Fatalf("items=%+v prefix=%q ok=%v", items, prefix, ok)
	}
}

func TestSlashSuggestionsRejectBareSlashLineWithArgs(t *testing.T) {
	if _, _, ok := slashSuggestions("/model claude", slash.Builtins()); ok {
		t.Fatal("arguments are not slash-name completions")
	}
}

func TestApplyCompleteSlashAndFile(t *testing.T) {
	got, col := applyComplete("/hotk", "/hotk", 5, completeItem{Value: "hotkeys"})
	if got != "/hotkeys " || col != len("/hotkeys ") {
		t.Fatalf("slash got %q col=%d", got, col)
	}
	got, _ = applyComplete("@unique", "@unique", 7, completeItem{Value: "@unique_alpha.go"})
	if got != "@unique_alpha.go " {
		t.Fatalf("file got %q", got)
	}
	got, _ = applyComplete("src/", "src/", 4, completeItem{Value: "src/pkg/", Dir: true})
	if got != "src/pkg/" {
		t.Fatalf("dir must not gain a trailing space, got %q", got)
	}
}

func TestFileSuggestionsAtPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "unique_alpha.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	items, prefix, ok := fileSuggestions("@unique", dir, true)
	if !ok || prefix != "@unique" || len(items) != 1 {
		t.Fatalf("items=%+v prefix=%q ok=%v", items, prefix, ok)
	}
	if !strings.HasPrefix(items[0].Value, "@unique_alpha.go") {
		t.Fatalf("value=%q", items[0].Value)
	}
}
