package theme

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSystemLightBackgroundIsNotDark(t *testing.T) {
	th := System(SystemInput{
		Background:     &RGB{R: 255, G: 255, B: 255},
		Foreground:     &RGB{},
		AppearanceHint: "dark",
	})
	if th.Appearance != "light" {
		t.Fatalf("appearance = %q, want light", th.Appearance)
	}
	if th.Accent != "#8268c4" || th.Error != "#dc2c44" {
		t.Fatalf("accent=%q error=%q", th.Accent, th.Error)
	}
	if th.Assistant != "" {
		t.Fatalf("text = %q, want terminal default", th.Assistant)
	}
	dark := Load("dark", "", "")
	if th.Accent == dark.Accent || th.Name == "dark" {
		t.Fatalf("light background used dark: %+v", th)
	}
}

func TestSystemZeroSaturationIsGray(t *testing.T) {
	zero := 0.0
	th := System(SystemInput{Background: &RGB{}, Saturation: &zero})
	if th.Appearance != "dark" || th.Error != "#9d9d9d" {
		t.Fatalf("appearance=%q error=%q", th.Appearance, th.Error)
	}
}

func TestSystemUsesPaletteHues(t *testing.T) {
	pal := []RGB{
		{0, 0, 0}, {255, 85, 85}, {80, 250, 123}, {241, 250, 140},
		{189, 147, 249}, {255, 121, 198}, {139, 233, 253}, {255, 255, 255},
		{98, 114, 164}, {255, 110, 110}, {90, 247, 142}, {255, 255, 170},
		{202, 169, 250}, {255, 146, 208}, {154, 237, 254}, {230, 230, 230},
	}
	th := System(SystemInput{
		Background: &RGB{R: 40, G: 42, B: 54},
		Foreground: &RGB{R: 248, G: 248, B: 242},
		Palette:    pal,
	})
	if th.Appearance != "dark" || th.Accent != "#ff5abe" || th.Error != "#f9756f" || th.Assistant != "" {
		t.Fatalf("appearance=%q accent=%q error=%q text=%q", th.Appearance, th.Accent, th.Error, th.Assistant)
	}
}

func TestSystemIndexedFallback(t *testing.T) {
	th := System(SystemInput{AppearanceHint: "light"})
	if th.Name != "system" || th.Appearance != "light" {
		t.Fatalf("%+v", th)
	}
	if th.Error != "1" || th.Assistant != "" || th.Colors["userMessageBg"] != "" {
		t.Fatalf("error=%q text=%q panel=%q", th.Error, th.Assistant, th.Colors["userMessageBg"])
	}
	if !slices.Contains(th.Dim, "muted") {
		t.Fatalf("dim = %v", th.Dim)
	}
}

func TestSystemNameIsReserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "system.json")
	body := `{"name":"system","accent":"#010101","user":"1","assistant":"2","tool":"3","error":"4","muted":"5"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	th := LoadWith(LoadOptions{
		Name:        "system",
		Extra:       []string{path},
		NoDiscovery: true,
		Terminal: SystemInput{
			Background:     &RGB{R: 255, G: 255, B: 255},
			Foreground:     &RGB{},
			AppearanceHint: "dark",
		},
	})
	if th.Accent != "#8268c4" {
		t.Fatalf("disk theme won: %+v", th)
	}
	names := NamesWith(LoadOptions{Extra: []string{path}, NoDiscovery: true})
	if names[0] != "system" {
		t.Fatalf("names = %v", names)
	}
}

func TestExplicitDarkIgnoresTerminal(t *testing.T) {
	th := LoadWith(LoadOptions{
		Name:     "dark",
		Terminal: SystemInput{Background: &RGB{R: 255, G: 255, B: 255}},
	})
	if th.Name != "dark" || th.Accent != "205" {
		t.Fatalf("%+v", th)
	}
}

func TestColorFgBgAppearance(t *testing.T) {
	if got := ColorFgBgAppearance("15;0"); got != "dark" {
		t.Fatalf("0 = %q", got)
	}
	if got := ColorFgBgAppearance("0;15"); got != "light" {
		t.Fatalf("15 = %q", got)
	}
	if got := ColorFgBgAppearance("0;8"); got != "dark" {
		t.Fatalf("8 = %q", got)
	}
	if got := ColorFgBgAppearance("default"); got != "" {
		t.Fatalf("default = %q", got)
	}
}
