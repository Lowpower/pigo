package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadColorSyntax(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ink.json")
	body := `{
		"name": "ink",
		"vars": {"primary": "#0af", "soft": "okhsl(0 0% 100%)"},
		"colors": {
			"accent": "primary",
			"error": "oklch(0% 0 0)",
			"text": "soft",
			"muted": 240,
			"userMessageText": "",
			"toolTitle": "#00ff00"
		}
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := LoadWithResult(LoadOptions{Name: "ink", Extra: []string{path}, NoDiscovery: true})
	if len(res.Errors) != 0 {
		t.Fatal(res.Errors)
	}
	th := res.Theme
	if th.Accent != "#00aaff" {
		t.Fatalf("accent = %q", th.Accent)
	}
	if th.Error != "#000000" {
		t.Fatalf("error = %q", th.Error)
	}
	if th.Assistant != "#ffffff" {
		t.Fatalf("text = %q", th.Assistant)
	}
	if th.Muted != "240" {
		t.Fatalf("muted = %q", th.Muted)
	}
	if th.User != "" {
		t.Fatalf("user = %q, want terminal default", th.User)
	}
	if th.Tool != "#00ff00" {
		t.Fatalf("tool = %q", th.Tool)
	}
}

func TestLoadOklchOutOfGamutMapsToWhite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hot.json")
	body := `{
		"name": "hot",
		"colors": {"accent": "oklch(100% 0.3 150)", "error": "#f00"}
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := LoadWithResult(LoadOptions{Name: "hot", Extra: []string{path}, NoDiscovery: true})
	if len(res.Errors) != 0 {
		t.Fatal(res.Errors)
	}
	if res.Theme.Accent != "#ffffff" {
		t.Fatalf("accent = %q", res.Theme.Accent)
	}
	if res.Theme.Error != "#ff0000" {
		t.Fatalf("error = %q", res.Theme.Error)
	}
}

func TestLoadAppearance(t *testing.T) {
	dir := t.TempDir()
	darkPath := filepath.Join(dir, "ink.json")
	darkBody := `{
		"name": "ink",
		"colors": {"text": "#eeeeee", "selectedBg": "#111111", "accent": "#ffffff"}
	}`
	if err := os.WriteFile(darkPath, []byte(darkBody), 0o644); err != nil {
		t.Fatal(err)
	}
	dark := LoadWithResult(LoadOptions{Name: "ink", Extra: []string{darkPath}, NoDiscovery: true})
	if len(dark.Errors) != 0 {
		t.Fatal(dark.Errors)
	}
	if dark.Theme.Appearance != "dark" {
		t.Fatalf("detected appearance = %q", dark.Theme.Appearance)
	}

	lightPath := filepath.Join(dir, "paper.json")
	lightBody := `{
		"name": "paper",
		"colors": {"text": "#222222", "selectedBg": "#f7f7f7", "accent": "#111111"}
	}`
	if err := os.WriteFile(lightPath, []byte(lightBody), 0o644); err != nil {
		t.Fatal(err)
	}
	light := LoadWithResult(LoadOptions{Name: "paper", Extra: []string{lightPath}, NoDiscovery: true})
	if len(light.Errors) != 0 {
		t.Fatal(light.Errors)
	}
	if light.Theme.Appearance != "light" {
		t.Fatalf("detected appearance = %q", light.Theme.Appearance)
	}

	forcedPath := filepath.Join(dir, "forced.json")
	forcedBody := `{
		"name": "forced",
		"appearance": "light",
		"colors": {"text": "#eeeeee", "selectedBg": "#111111", "accent": "#ffffff"}
	}`
	if err := os.WriteFile(forcedPath, []byte(forcedBody), 0o644); err != nil {
		t.Fatal(err)
	}
	forced := LoadWithResult(LoadOptions{Name: "forced", Extra: []string{forcedPath}, NoDiscovery: true})
	if len(forced.Errors) != 0 {
		t.Fatal(forced.Errors)
	}
	if forced.Theme.Appearance != "light" {
		t.Fatalf("declared appearance = %q", forced.Theme.Appearance)
	}

	badPath := filepath.Join(dir, "bad-look.json")
	if err := os.WriteFile(badPath, []byte(`{"name":"bad-look","appearance":"blue","accent":"1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := LoadWithResult(LoadOptions{Name: "system", Extra: []string{badPath}, NoDiscovery: true})
	if bad.Theme.Name == "bad-look" {
		t.Fatal("invalid appearance was loaded")
	}
	if !strings.Contains(strings.Join(bad.Errors, "\n"), `invalid field "appearance"`) {
		t.Fatalf("errors = %v", bad.Errors)
	}
}

func TestLoadRejectsBadColors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	body := `{
		"name": "bad",
		"vars": {"a": "b", "b": "a"},
		"colors": {
			"accent": "nope",
			"error": "oklch(2 0 0)",
			"text": "a",
			"muted": 256
		}
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := LoadWithResult(LoadOptions{Name: "system", Extra: []string{path}, NoDiscovery: true})
	if res.Theme.Name == "bad" {
		t.Fatal("invalid theme was loaded")
	}
	joined := strings.Join(res.Errors, "\n")
	for _, want := range []string{path, `invalid color "accent"`, `invalid color "error"`, `invalid color "text"`, `invalid color "muted"`, "unknown variable", "circular variable"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestHex(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"#abc", "#aabbcc"},
		{"#12AB34", "#12ab34"},
		{"0", "#000000"},
		{"15", "#ffffff"},
		{"178", "#d7af00"},
		{"244", "#808080"},
		{"256", ""},
		{"invalid", ""},
	}
	for _, tc := range cases {
		if got := Hex(tc.in); got != tc.want {
			t.Errorf("Hex(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
