package theme

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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
	if th.Appearance != "dark" || th.Accent != "#f16cb9" || th.Error != "#f9756f" || th.Assistant != "" {
		t.Fatalf("appearance=%q accent=%q error=%q text=%q", th.Appearance, th.Accent, th.Error, th.Assistant)
	}
}

func TestSystemPastelPaletteKeepsChroma(t *testing.T) {
	pal := []RGB{
		rgbHex(t, "#51576d"), rgbHex(t, "#e78284"), rgbHex(t, "#a6d189"), rgbHex(t, "#e5c890"),
		rgbHex(t, "#8caaee"), rgbHex(t, "#f4b8e4"), rgbHex(t, "#81c8be"), rgbHex(t, "#b5bfe2"),
		rgbHex(t, "#626880"), rgbHex(t, "#e67172"), rgbHex(t, "#8ec772"), rgbHex(t, "#d9ba73"),
		rgbHex(t, "#7b9ef0"), rgbHex(t, "#f2a4db"), rgbHex(t, "#5abfb5"), rgbHex(t, "#a5adce"),
	}
	background := rgbHex(t, "#303446")
	foreground := rgbHex(t, "#c6d0f5")
	th := System(SystemInput{
		Background: &background,
		Foreground: &foreground,
		Palette:    pal,
	})
	pink := pal[5]
	accent := rgbHex(t, th.Accent)
	if oklabLightness(accent) >= oklabLightness(pink)-0.05 {
		t.Fatalf("accent lightness %v, pink %v", oklabLightness(accent), oklabLightness(pink))
	}
	_, accentC := oklabLC(accent)
	_, pinkC := oklabLC(pink)
	if accentC > pinkC*1.03 {
		t.Fatalf("accent chroma %v, pink %v", accentC, pinkC)
	}
	for _, token := range []string{"userMessageBg", "customMessageBg"} {
		_, chroma := oklabLC(rgbHex(t, th.Colors[token]))
		if chroma > 0.1 {
			t.Fatalf("%s chroma %v color %s", token, chroma, th.Colors[token])
		}
	}
	if th.Accent != "#cc92bd" || th.Colors["userMessageBg"] != "#283d73" || th.Colors["customMessageBg"] != "#5d2b52" {
		t.Fatalf("accent=%s user=%s custom=%s", th.Accent, th.Colors["userMessageBg"], th.Colors["customMessageBg"])
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

func rgbHex(t *testing.T, hex string) RGB {
	t.Helper()
	raw := strings.TrimPrefix(hex, "#")
	n, err := strconv.ParseUint(raw, 16, 32)
	if err != nil || len(raw) != 6 {
		t.Fatalf("bad hex %q", hex)
	}
	return RGB{R: int(n >> 16), G: int(n >> 8 & 0xff), B: int(n & 0xff)}
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
