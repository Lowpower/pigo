package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadBuiltin(t *testing.T) {
	light := Load("light", "", "")
	if light.Name != "light" || light.Appearance != "light" || light.Accent != formatHex(okhslToRgb(295, 0.60, 0.46)) {
		t.Fatalf("light = %+v", light)
	}
	if light.Colors["text"] != formatHex(okhslToRgb(225, 0.05, 0.27)) || light.ExportPageBg == "" {
		t.Fatalf("light tokens = %+v", light)
	}
	dark := Load("dark", "", "")
	if dark.Name != "dark" || dark.Appearance != "dark" || dark.Accent != formatHex(okhslToRgb(295, 0.50, 0.67)) {
		t.Fatalf("dark = %+v", dark)
	}
	if dark.Error != formatHex(okhslToRgb(20, 0.72, 0.67)) {
		t.Fatalf("dark error = %s", dark.Error)
	}
	def := Load("default", "", "")
	if def.Name != "default" || def.Accent != "205" || def.Colors != nil {
		t.Fatalf("default = %+v", def)
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
			"selectedBg": "#2d2d30"
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

func TestLoadThemeStripsBOM(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.Mkdir(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "\ufeff" + `{"name":"bommed","user":"#111111","assistant":"#222222","tool":"#333333","error":"#ff0000","muted":"240","accent":"#00aaff"}`
	if err := os.WriteFile(filepath.Join(themes, "bommed.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	th := Load("bommed", "", dir)
	if th.Name != "bommed" || th.Accent != "#00aaff" {
		t.Fatalf("%+v", th)
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

func TestThemeSchemaKeyAccepted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.json")
	body := `{
		"$schema": "https://raw.githubusercontent.com/Lowpower/pigo/main/docs/schemas/theme.schema.json",
		"name": "mine",
		"user": 42,
		"accent": "1"
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := LoadWithResult(LoadOptions{Name: "mine", Extra: []string{path}, NoDiscovery: true})
	if len(res.Errors) != 0 {
		t.Fatal(res.Errors)
	}
	if res.Theme.Name != "mine" || res.Theme.User != "42" || res.Theme.Accent != "1" {
		t.Fatalf("%+v", res.Theme)
	}
}

func TestUnknownThemeFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	body := `{
		"name": "bad",
		"extra": true,
		"accent": "1",
		"colors": {"accent": "#fff", "scrollbarThmb": "#000"},
		"export": {"pageBg": "#111", "nope": "#222"}
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := LoadWithResult(LoadOptions{Name: "system", Extra: []string{path}, NoDiscovery: true})
	if res.Theme.Name == "bad" {
		t.Fatal("invalid theme was loaded")
	}
	joined := strings.Join(res.Errors, "\n")
	for _, want := range []string{path, `unknown field "extra"`, `unknown color "scrollbarThmb"`, `unknown field "nope"`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	names := NamesWith(LoadOptions{Extra: []string{path}, NoDiscovery: true})
	for _, n := range names {
		if n == "bad" {
			t.Fatalf("invalid theme listed: %v", names)
		}
	}
}

func TestHighContrastExampleLoads(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "examples", "themes", "high-contrast.json")
	res := LoadWithResult(LoadOptions{Name: "high-contrast", Extra: []string{path}, NoDiscovery: true})
	if len(res.Errors) != 0 {
		t.Fatal(res.Errors)
	}
	if res.Theme.Name != "high-contrast" || res.Theme.Accent != "213" {
		t.Fatalf("%+v", res.Theme)
	}
}

func TestThemeSchemaMatchesAllowList(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "docs", "schemas")
	raw, err := os.ReadFile(filepath.Join(root, "theme.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "pi.dev") {
		t.Fatal("theme schema points at pi.dev")
	}
	var doc schemaDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	const wantID = "https://raw.githubusercontent.com/Lowpower/pigo/main/docs/schemas/theme.schema.json"
	if doc.ID != wantID {
		t.Fatalf("$id = %s", doc.ID)
	}
	if doc.Additional == nil || *doc.Additional {
		t.Fatal("theme schema must reject unknown keys")
	}
	if keys := mapKeys(doc.Properties); !sameKeys(keys, themeFileKeys) {
		t.Fatalf("top-level properties = %v", keys)
	}
	colors := nestedSchema(t, doc.Properties["colors"])
	if colors.Additional == nil || *colors.Additional {
		t.Fatal("colors must reject unknown tokens")
	}
	if !sameKeys(mapKeys(colors.Properties), colorTokenSet()) {
		t.Fatalf("color tokens = %v", mapKeys(colors.Properties))
	}
	export := nestedSchema(t, doc.Properties["export"])
	if export.Additional == nil || *export.Additional {
		t.Fatal("export must reject unknown keys")
	}
	if !sameKeys(mapKeys(export.Properties), themeExportKeys) {
		t.Fatalf("export keys = %v", mapKeys(export.Properties))
	}
	for _, name := range []string{"settings.schema.json", "models.schema.json", "keybindings.schema.json"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "pi.dev") {
			t.Fatalf("%s points at pi.dev", name)
		}
		var other struct {
			ID string `json:"$id"`
		}
		if err := json.Unmarshal(b, &other); err != nil {
			t.Fatal(err)
		}
		want := "https://raw.githubusercontent.com/Lowpower/pigo/main/docs/schemas/" + name
		if other.ID != want {
			t.Fatalf("%s $id = %s", name, other.ID)
		}
	}
}

type schemaDoc struct {
	ID         string                     `json:"$id"`
	Additional *bool                      `json:"additionalProperties"`
	Properties map[string]json.RawMessage `json:"properties"`
}

func nestedSchema(t *testing.T, raw json.RawMessage) schemaDoc {
	t.Helper()
	var doc schemaDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func mapKeys[M ~map[string]V, V any](m M) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func sameKeys(got map[string]bool, want map[string]bool) bool {
	if len(got) != len(want) {
		return false
	}
	for k := range want {
		if !got[k] {
			return false
		}
	}
	return true
}
