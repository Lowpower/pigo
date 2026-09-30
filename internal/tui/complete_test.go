package tui

import (
	"testing"

	"github.com/Lowpower/pigo/internal/slash"
)

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
