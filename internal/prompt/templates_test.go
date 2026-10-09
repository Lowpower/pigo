package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubstituteArgsPiPatterns(t *testing.T) {
	args := []string{"one", "two", "three"}
	cases := []struct {
		in, want string
	}{
		{"use $1 and $2", "use one and two"},
		{"all $@", "all one two three"},
		{"all $ARGUMENTS", "all one two three"},
		{"${1:-fallback}", "one"},
		{"${9:-fallback}", "fallback"},
		{"${@:-none}", "one two three"},
		{"${@:2}", "two three"},
		{"${@:2:1}", "two"},
		{"missing $4", "missing "},
	}
	for _, c := range cases {
		got := SubstituteArgs(c.in, args)
		if got != c.want {
			t.Errorf("SubstituteArgs(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestParseCommandArgsQuotes(t *testing.T) {
	got := ParseCommandArgs(`foo "bar baz" 'x y'`)
	if len(got) != 3 || got[0] != "foo" || got[1] != "bar baz" || got[2] != "x y" {
		t.Fatalf("%q", got)
	}
}

func TestDiscoverTemplatesMalformedFrontmatter(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.md")
	body := "---\ndescription: Broken: unquoted colon\n---\nDo something.\n"
	if err := os.WriteFile(invalid, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "valid.md"), []byte("Valid prompt content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, diags := DiscoverTemplates("", "", []string{dir}, false, false)
	if len(got) != 1 || got[0].Name != "valid" {
		t.Fatalf("templates = %+v", got)
	}
	if got[0].Description != "Valid prompt content." {
		t.Fatalf("description = %q", got[0].Description)
	}
	if len(diags) != 1 || diags[0].Path != invalid || diags[0].Message == "" {
		t.Fatalf("diagnostics = %+v", diags)
	}
	warning := FormatWarning(diags[0])
	if !strings.HasPrefix(warning, "Warning: malformed prompt template frontmatter in "+invalid+": ") {
		t.Fatalf("warning = %q", warning)
	}
}

func TestDiscoverTemplatesNoFrontmatterUsesFilename(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("a", 70)
	if err := os.WriteFile(filepath.Join(dir, "plain.md"), []byte("\n"+long+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, diags := DiscoverTemplates("", "", []string{dir}, false, false)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	if len(got) != 1 || got[0].Name != "plain" {
		t.Fatalf("templates = %+v", got)
	}
	if got[0].Description != long[:60]+"..." {
		t.Fatalf("description = %q", got[0].Description)
	}
}

func TestDiscoverTemplatesValidFrontmatterFields(t *testing.T) {
	dir := t.TempDir()
	body := "---\ndescription: Review the named files\nargument-hint: <path>…\n---\n\nReview these paths: $@\n"
	if err := os.WriteFile(filepath.Join(dir, "review.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, diags := DiscoverTemplates("", "", []string{dir}, false, false)
	if len(diags) != 0 || len(got) != 1 {
		t.Fatalf("templates = %+v diagnostics = %+v", got, diags)
	}
	if got[0].Name != "review" || got[0].Description != "Review the named files" || got[0].ArgumentHint != "<path>…" {
		t.Fatalf("%+v", got[0])
	}
	if got[0].Content != "Review these paths: $@" {
		t.Fatalf("content = %q", got[0].Content)
	}
}

func TestDiscoverTemplatesBlockScalarWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	body := "---\ndescription: >\n  Review a change\n  for tests.\nargument-hint: <path>\n---"
	if err := os.WriteFile(filepath.Join(dir, "review.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, diags := DiscoverTemplates("", "", []string{dir}, false, false)
	if len(diags) != 0 || len(got) != 1 {
		t.Fatalf("templates = %+v diagnostics = %+v", got, diags)
	}
	if got[0].Description != "Review a change for tests.\n" || got[0].ArgumentHint != "<path>" || got[0].Content != "" {
		t.Fatalf("%+v", got[0])
	}
}

func TestDiscoverTemplatesUnclosedFrontmatterStillLoads(t *testing.T) {
	dir := t.TempDir()
	body := "---\ndescription: not closed\nDo the thing.\n"
	if err := os.WriteFile(filepath.Join(dir, "open.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, diags := DiscoverTemplates("", "", []string{dir}, false, false)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	if len(got) != 1 || got[0].Name != "open" {
		t.Fatalf("templates = %+v", got)
	}
}

func TestExpandTemplate(t *testing.T) {
	tpls := []Template{{Name: "review", Content: "Review $1"}}
	got, ok := ExpandTemplate("/review pkg/", tpls)
	if !ok || got != "Review pkg/" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := ExpandTemplate("/nope", tpls); ok {
		t.Fatal("unknown template should not expand")
	}
}
