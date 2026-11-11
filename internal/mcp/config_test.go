package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectOverrideAndTrust(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	writeJSON(t, filepath.Join(agent, "mcp.json"), `{
		"mcpServers": {
			"tools": {"command": "tool-server", "args": ["--stdio"], "env": {"TOKEN": "secret"}, "description": "user tools"},
			"other": {"url": "http://127.0.0.1:9/mcp", "exposure": "direct"}
		}
	}`)
	writeJSON(t, filepath.Join(cwd, ".pigo", "mcp.json"), `{
		"mcpServers": {
			"tools": {"enabled": false},
			"missing": {"exposure": "direct"}
		}
	}`)

	got, diags := Load(agent, cwd, true)
	if len(got) != 2 {
		t.Fatalf("servers=%d diags=%v", len(got), diags)
	}
	tools := serverNamed(got, "tools")
	if tools.Config.Enabled {
		t.Fatal("project enabled:false did not apply")
	}
	if tools.Config.Command != "tool-server" || tools.Config.Env["TOKEN"] != "secret" {
		t.Fatalf("override dropped the user server: %+v", tools.Config)
	}
	if tools.Config.ExposureValue() != "codemode" {
		t.Fatalf("exposure=%s", tools.Config.ExposureValue())
	}
	if !serverNamed(got, "other").Config.BlocksFirstPrompt() {
		t.Fatal("direct server should block the first prompt")
	}
	if tools.Config.BlocksFirstPrompt() {
		t.Fatal("codemode server should not block the first prompt")
	}
	var sawMissing bool
	for _, d := range diags {
		if strings.Contains(d.Error(), "missing") && strings.Contains(d.Error(), "no user server") {
			sawMissing = true
		}
	}
	if !sawMissing {
		t.Fatalf("diags=%v", diags)
	}

	untrusted, _ := Load(agent, cwd, false)
	if !serverNamed(untrusted, "tools").Config.Enabled {
		t.Fatal("untrusted project config was applied")
	}
}

func TestProjectReplaceAndBadOverride(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	writeJSON(t, filepath.Join(agent, "mcp.json"), `{
		"mcpServers": {"tools": {"command": "tool-server", "auth": {"provider": "demo"}}}
	}`)
	writeJSON(t, filepath.Join(cwd, ".pigo", "mcp.json"), `{
		"mcpServers": {
			"tools": {"url": "http://127.0.0.1:9/mcp", "exposure": "deferred"},
			"local": {"command": "local", "headers": {"Authorization": "Bearer x"}},
			"bad": {"enabled": false, "env": {"A": "b"}}
		}
	}`)
	got, diags := Load(agent, cwd, true)
	tools := serverNamed(got, "tools")
	if tools.Config.URL != "http://127.0.0.1:9/mcp" || tools.Config.Command != "" {
		t.Fatalf("replace=%+v", tools.Config)
	}
	if tools.Config.ExposureValue() != "deferred" {
		t.Fatalf("exposure=%s", tools.Config.ExposureValue())
	}
	if !strings.Contains(string(tools.Config.Raw), "127.0.0.1") {
		t.Fatalf("raw=%s", tools.Config.Raw)
	}
	local := serverNamed(got, "local")
	if local.Config.Headers["Authorization"] != "Bearer x" {
		t.Fatalf("headers=%v", local.Config.Headers)
	}
	var sawBad bool
	for _, d := range diags {
		if strings.Contains(d.Error(), `"bad"`) && strings.Contains(d.Error(), ".pigo/mcp.json") {
			sawBad = true
		}
	}
	if !sawBad {
		t.Fatalf("diags=%v", diags)
	}
}

func TestFileBeatsExtensionAndServerNamesCollide(t *testing.T) {
	user := []Server{{
		Name:   "demo",
		Config: Config{Command: "from-file", Enabled: true},
		Source: SourceUser,
	}, {
		Name:   "ok",
		Config: Config{Command: "ok", Enabled: true},
		Source: SourceUser,
	}}
	regs := []Server{{
		Name:   "demo",
		Config: Config{Command: "from-ext", Enabled: true, URL: ""},
		Source: SourceExtension,
	}, {
		Name:   "ok_dash",
		Config: Config{Command: "x", Enabled: true},
		Source: SourceExtension,
	}}
	// ok and ok_dash do not collide. Add a real collision.
	regs = append(regs, Server{
		Name:   "foo-bar",
		Config: Config{Command: "a", Enabled: true},
		Source: SourceExtension,
	}, Server{
		Name:   "foo_bar",
		Config: Config{Command: "b", Enabled: true},
		Source: SourceExtension,
	})
	got, diags := MergeExtensions(user, regs)
	demo := serverNamed(got, "demo")
	if demo.Config.Command != "from-file" {
		t.Fatalf("file lost to extension: %+v", demo)
	}
	if serverNamed(got, "ok").Name != "ok" {
		t.Fatal("missing ok")
	}
	for _, s := range got {
		if s.Name == "foo-bar" || s.Name == "foo_bar" {
			t.Fatalf("collision loaded: %+v", got)
		}
	}
	var saw bool
	for _, d := range diags {
		if strings.Contains(d.Error(), "foo-bar") && strings.Contains(d.Error(), "foo_bar") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("diags=%v", diags)
	}
}

func TestToolExposureExactBeatsPattern(t *testing.T) {
	cfg := Config{
		Exposure: "hidden",
		ToolExposure: []ExposureRule{
			{Pattern: "get_*", Exposure: "codemode"},
			{Pattern: "*", Exposure: "deferred"},
			{Pattern: "search_code", Exposure: "direct"},
		},
	}
	if got := cfg.ExposureFor("search_code"); got != "direct" {
		t.Fatalf("exact=%s", got)
	}
	if got := cfg.ExposureFor("get_file"); got != "codemode" {
		t.Fatalf("pattern=%s", got)
	}
	if got := cfg.ExposureFor("other"); got != "deferred" {
		t.Fatalf("star=%s", got)
	}
	if !cfg.BlocksFirstPrompt() {
		t.Fatal("a direct toolExposure should block the first prompt")
	}
}

func TestRegisteredToolNames(t *testing.T) {
	plain := RegisteredToolNames("dev-radius", []string{"list-files"})
	if plain["list-files"] != "mcp__dev_radius__list_files" {
		t.Fatalf("name=%s", plain["list-files"])
	}
	if Namespace("dev-radius") != "mcp__dev_radius" {
		t.Fatal(Namespace("dev-radius"))
	}
	if CanonNamespace("dev-radius") != "mcp__dev_radius" || CanonNamespace("mcp__dev-radius") != "mcp__dev_radius" {
		t.Fatal("namespace fold")
	}
	first := RegisteredToolNames("srv", []string{"foo-bar", "foo_bar"})
	second := RegisteredToolNames("srv", []string{"foo-bar", "foo_bar"})
	if first["foo-bar"] != second["foo-bar"] || first["foo-bar"] == first["foo_bar"] {
		t.Fatalf("names=%v", first)
	}
	if !strings.HasPrefix(first["foo-bar"], "mcp__srv__foo_bar__") {
		t.Fatalf("suffix=%s", first["foo-bar"])
	}
	alias := ToolAlias("dev-radius", "list-files", plain["list-files"])
	if alias != "mcp__dev-radius__list-files" {
		t.Fatalf("alias=%s", alias)
	}
}

func TestSectionLine(t *testing.T) {
	cfg := Config{Description: "Search code.", Exposure: ""}
	line := SectionLine("github", cfg, "ignored\nsecond")
	if line != "github: called from codemode scripts. Search code." {
		t.Fatalf("line=%q", line)
	}
	cfg.Description = ""
	line = SectionLine("github", cfg, "From the server.\nMore")
	if line != "github: called from codemode scripts. From the server." {
		t.Fatalf("line=%q", line)
	}
}

func TestServerRef(t *testing.T) {
	m := New([]Server{{Name: "dev-radius", Config: Config{Command: "x", Enabled: true}}}, "")
	for _, ref := range []string{
		"dev-radius", "dev_radius", "mcp__dev-radius", "mcp__dev_radius",
		"mcp__dev_radius__list_files", "mcp__dev-radius__list-files",
	} {
		if m.serverForRef(ref) == nil {
			t.Fatalf("miss %s", ref)
		}
	}
	if m.serverForRef("other") != nil || m.serverForRef("device") != nil {
		t.Fatal("false match")
	}
}

func serverNamed(servers []Server, name string) Server {
	for _, s := range servers {
		if s.Name == name {
			return s
		}
	}
	return Server{}
}

func writeJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
