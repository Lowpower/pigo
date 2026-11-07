package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDefaultTools(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{name: "modifiers on builtin default", in: []string{"+grep", "-write"}, want: "read,bash,edit,grep"},
		{name: "plain replaces default", in: []string{"grep", "find"}, want: "grep,find"},
		{name: "empty replaces with nothing", in: []string{}, want: ""},
		{name: "plain plus modifiers", in: []string{"read", "+grep", "-read"}, want: "grep"},
		{name: "plus then minus", in: []string{"+write", "-write"}, want: "read,bash,edit"},
		{name: "minus then plus", in: []string{"-write", "+write"}, want: "read,bash,edit,write"},
		{name: "duplicate plus", in: []string{"+read"}, want: "read,bash,edit,write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(ResolveDefaultTools(tc.in), ",")
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestMergeDefaultTools(t *testing.T) {
	user := []string{"grep", "find"}
	mods := []string{"+ls", "-find"}
	merged := MergeDefaultTools(&user, &mods)
	if strings.Join(*merged, ",") != "grep,find,+ls,-find" {
		t.Fatalf("raw = %v", *merged)
	}
	if strings.Join(ResolveDefaultTools(*merged), ",") != "grep,ls" {
		t.Fatalf("resolved = %v", ResolveDefaultTools(*merged))
	}

	replace := []string{"read", "bash"}
	replaced := MergeDefaultTools(&user, &replace)
	if strings.Join(*replaced, ",") != "read,bash" {
		t.Fatalf("replace = %v", *replaced)
	}

	onlyMods := []string{"+ls", "-write"}
	stacked := MergeDefaultTools(nil, &onlyMods)
	if strings.Join(*stacked, ",") != "+ls,-write" {
		t.Fatalf("raw unset base = %v", *stacked)
	}
	if strings.Join(ResolveDefaultTools(*stacked), ",") != "read,bash,edit,ls" {
		t.Fatalf("resolved unset base = %v", ResolveDefaultTools(*stacked))
	}

	empty := []string{}
	cleared := MergeDefaultTools(&user, &empty)
	if cleared == nil || len(*cleared) != 0 {
		t.Fatalf("empty override = %v", cleared)
	}
	if n := len(ResolveDefaultTools(*cleared)); n != 0 {
		t.Fatalf("empty resolves to %d names", n)
	}

	kept := MergeDefaultTools(&user, nil)
	if strings.Join(*kept, ",") != "grep,find" {
		t.Fatalf("nil override = %v", *kept)
	}
}

func TestValidateToolList(t *testing.T) {
	if err := ValidateToolList([]string{"read", "grep*"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateToolList([]string{"+grep", "-write"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateToolList(nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateToolList([]string{"read", "+grep"}); err == nil {
		t.Fatal("mixed list should fail")
	}
	if err := ValidateToolList([]string{"+ba*"}); err == nil {
		t.Fatal("pattern in modifier should fail")
	}
}

func TestLoadModifierDefaultTools(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"defaultTools":["+grep","-write"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultTools == nil || strings.Join(*cfg.DefaultTools, ",") != "+grep,-write" {
		t.Fatalf("raw = %v", cfg.DefaultTools)
	}
	if strings.Join(cfg.InitialBuiltinTools(), ",") != "read,bash,edit,grep" {
		t.Fatalf("resolved = %v", cfg.InitialBuiltinTools())
	}
}

func TestToolNameMatches(t *testing.T) {
	if !ToolNameMatches("mcp__radius__*", "mcp__radius__foo") {
		t.Fatal("glob should match")
	}
	if ToolNameMatches("mcp__radius__*", "mcp__other__foo") {
		t.Fatal("glob should not match a different server")
	}
	if !ToolNameMatches("read", "read") || ToolNameMatches("read", "read2") {
		t.Fatal("exact match")
	}
	got := MatchingToolNames([]string{"ba*", "grep"}, []string{"read", "bash", "grep", "find"})
	if strings.Join(got, ",") != "bash,grep" {
		t.Fatalf("matching = %v", got)
	}
	left := RejectToolNames([]string{"read", "bash", "write"}, []string{"ba*", "write"})
	if strings.Join(left, ",") != "read" {
		t.Fatalf("rejected = %v", left)
	}
}
