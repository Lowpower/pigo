package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBuiltin(t *testing.T) {
	th := Load("light", "", "")
	if th.Name != "light" || th.Accent != "162" {
		t.Fatalf("%+v", th)
	}
}

func TestLoadLegacyFile(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.Mkdir(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"legacy","user":"#111111","assistant":"#222222","tool":"#333333","error":"#ff0000","muted":"240","accent":"#00aaff"}`
	if err := os.WriteFile(filepath.Join(themes, "legacy.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	th := Load("legacy", "", dir)
	if th.Name != "legacy" || th.Accent != "#00aaff" || th.User != "#111111" {
		t.Fatalf("%+v", th)
	}
}

func TestLoadPiSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.json")
	body := `{
		"name": "mine",
		"vars": {"primary": "#00aaff", "gray": 242},
		"colors": {
			"accent": "primary",
			"error": "#ff0000",
			"muted": "gray",
			"text": "",
			"userMessageText": "#abcdef",
			"toolTitle": "#00ff00",
			"thinkingXhigh": "#ff00ff",
			"selectedBg": "#2d2d30",
			"unknownToken": "#ffffff"
		}
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	th := LoadWith(LoadOptions{Name: "mine", Extra: []string{path}})
	if th.Name != "mine" {
		t.Fatalf("name=%s", th.Name)
	}
	if th.Accent != "#00aaff" {
		t.Fatalf("accent=%s", th.Accent)
	}
	if th.Error != "#ff0000" {
		t.Fatalf("error=%s", th.Error)
	}
	if th.Muted != "242" {
		t.Fatalf("muted=%s", th.Muted)
	}
	if th.User != "#abcdef" {
		t.Fatalf("user=%s", th.User)
	}
	if th.Tool != "#00ff00" {
		t.Fatalf("tool=%s", th.Tool)
	}
	if th.Colors["unknownToken"] != "#ffffff" {
		t.Fatalf("unknown token dropped: %+v", th.Colors)
	}
	if th.Colors["thinkingMax"] != "#ff00ff" {
		t.Fatalf("thinkingMax fallback = %q", th.Colors["thinkingMax"])
	}
	if th.Colors["scrollbarThumb"] != "#2d2d30" {
		t.Fatalf("scrollbarThumb fallback = %q", th.Colors["scrollbarThumb"])
	}
}

func TestLoadPiExportColors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exp.json")
	body := `{
		"name": "exp",
		"vars": {"bg": "#112233"},
		"colors": {"accent": "#00aaff", "error": "#ff0000", "text": "#eeeeee"},
		"export": {"pageBg": "bg", "cardBg": "#223344", "infoBg": 52}
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	th := LoadWith(LoadOptions{Name: "exp", Extra: []string{path}})
	if th.ExportPageBg != "#112233" || th.ExportCardBg != "#223344" || th.ExportInfoBg != "52" {
		t.Fatalf("export %+v", th)
	}
}

func TestNoDiscoverySkipsAgentDir(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.Mkdir(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "hidden.json"), []byte(`{"name":"hidden","accent":"#111111","user":"1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	th := LoadWith(LoadOptions{Name: "hidden", AgentDir: dir, NoDiscovery: true})
	if th.Name == "hidden" {
		t.Fatal("discovered hidden theme despite --no-themes")
	}
	th = LoadWith(LoadOptions{Name: "hidden", AgentDir: dir})
	if th.Name != "hidden" {
		t.Fatalf("want hidden, got %s", th.Name)
	}
}

func TestExtraThemeStillLoadsWithNoDiscovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cli.json")
	if err := os.WriteFile(path, []byte(`{"name":"cli","colors":{"accent":"#123456","error":"#ff0000"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	th := LoadWith(LoadOptions{Name: "cli", NoDiscovery: true, Extra: []string{path}})
	if th.Name != "cli" || th.Accent != "#123456" {
		t.Fatalf("%+v", th)
	}
}

func TestSlashPairUsesDarkSide(t *testing.T) {
	th := Load("light/dark", "", "")
	if th.Name != "dark" {
		t.Fatalf("name=%s", th.Name)
	}
}

func TestInvalidNameWithSlashDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"name":"a/b","colors":{"accent":"#fff"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	th := LoadWith(LoadOptions{Name: "a/b", Extra: []string{path}})
	if th.Name == "a/b" {
		t.Fatal("slash in theme name must be rejected")
	}
}

func TestNamesIncludesExtra(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "extra.json")
	if err := os.WriteFile(path, []byte(`{"name":"extra","user":"1","accent":"2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NamesWith(LoadOptions{Extra: []string{path}})
	found := false
	for _, n := range got {
		if n == "extra" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%v", got)
	}
}
