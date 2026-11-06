package pkgmgr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
)

func builtinByPath(rs []Resource) map[string]Resource {
	out := map[string]Resource{}
	for _, r := range rs {
		if r.Source == "builtin" {
			out[r.Path] = r
		}
	}
	return out
}

func TestResolveBuiltinExtensionsDefaultOn(t *testing.T) {
	m, err := Open(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := builtinByPath(rs)
	for _, name := range BuiltinNames {
		path := BuiltinPath(name)
		r, ok := got[path]
		if !ok || !r.Enabled || r.Scope != "user" || r.Origin != "top-level" || r.Type != KindExtensions {
			t.Fatalf("%s = %+v", path, r)
		}
	}
	if len(got) != len(BuiltinNames) {
		t.Fatalf("builtins = %+v", got)
	}
}

func TestResolveBuiltinUserDisable(t *testing.T) {
	agent := t.TempDir()
	writeSettingsJSON(t, agent, `{"extensions":["-builtin:mcp"]}`)
	m, err := Open(t.TempDir(), agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := builtinByPath(rs)
	if got[BuiltinPath("mcp")].Enabled {
		t.Fatal("mcp still enabled")
	}
	if !got[BuiltinPath("llama.cpp")].Enabled || !got[BuiltinPath("tool-search")].Enabled {
		t.Fatalf("others = %+v", got)
	}
}

func TestResolveBuiltinProjectOverridesUser(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	writeSettingsJSON(t, agent, `{"extensions":["-builtin:mcp","-builtin:tool-search"]}`)
	writeSettingsJSON(t, filepath.Join(cwd, ".pigo"), `{"extensions":["+builtin:mcp"]}`)
	m, err := Open(cwd, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := builtinByPath(rs)
	mcp := got[BuiltinPath("mcp")]
	if !mcp.Enabled || mcp.Scope != "project" {
		t.Fatalf("mcp = %+v", mcp)
	}
	search := got[BuiltinPath("tool-search")]
	if search.Enabled || search.Scope != "user" {
		t.Fatalf("tool-search = %+v", search)
	}
}

func TestResolveBuiltinUntrustedIgnoresProject(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	writeSettingsJSON(t, filepath.Join(cwd, ".pigo"), `{"extensions":["-builtin:mcp"]}`)
	m, err := Open(cwd, agent, false)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mcp := builtinByPath(rs)[BuiltinPath("mcp")]
	if !mcp.Enabled || mcp.Scope != "user" {
		t.Fatalf("mcp = %+v", mcp)
	}
}

func TestResolveBuiltinGlobDisable(t *testing.T) {
	agent := t.TempDir()
	writeSettingsJSON(t, agent, `{"extensions":["!builtin:*"]}`)
	m, err := Open(t.TempDir(), agent, false)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range builtinByPath(rs) {
		if r.Enabled {
			t.Fatalf("still enabled: %+v", r)
		}
	}
}

func TestApplyBuiltinFlagsMatrix(t *testing.T) {
	resolved := appendBuiltinResources(nil, nil, nil, false)
	all, err := ApplyBuiltinFlags(resolved, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if joinBuiltin(all) != "builtin:llama.cpp builtin:codemode builtin:tool-search builtin:mcp" {
		t.Fatalf("default = %s", joinBuiltin(all))
	}

	disabled := appendBuiltinResources(nil, []string{"-builtin:mcp"}, nil, false)
	got, err := ApplyBuiltinFlags(disabled, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(joinBuiltin(got), "builtin:mcp") {
		t.Fatalf("mcp present: %s", joinBuiltin(got))
	}
	if !strings.Contains(joinBuiltin(got), "builtin:llama.cpp") {
		t.Fatalf("llama missing: %s", joinBuiltin(got))
	}

	only, err := ApplyBuiltinFlags(resolved, true, false, []string{"builtin:tool-search"})
	if err != nil {
		t.Fatal(err)
	}
	if joinBuiltin(only) != "builtin:tool-search" {
		t.Fatalf("reenabled = %s", joinBuiltin(only))
	}

	none, err := ApplyBuiltinFlags(resolved, true, false, nil)
	if err != nil || len(none) != 0 {
		t.Fatalf("no-extensions = %+v err=%v", none, err)
	}
	if tools, err := ApplyBuiltinFlags(resolved, false, true, []string{"builtin:mcp"}); err != nil || len(tools) != 0 {
		t.Fatalf("no-tools = %+v err=%v", tools, err)
	}

	back, err := ApplyBuiltinFlags(disabled, false, false, []string{"builtin:mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joinBuiltin(back), "builtin:mcp") {
		t.Fatalf("cli did not add mcp back: %s", joinBuiltin(back))
	}

	_, err = ApplyBuiltinFlags(resolved, true, false, []string{"builtin:nope"})
	if err == nil || err.Error() != "unknown built-in extension: builtin:nope" {
		t.Fatalf("err=%v", err)
	}
}

func TestToggleBuiltinWritesSignedPattern(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	m, err := Open(cwd, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var mcp Resource
	for _, r := range rs {
		if r.Path == BuiltinPath("mcp") {
			mcp = r
		}
	}
	if mcp.Path == "" || !mcp.Enabled {
		t.Fatalf("mcp = %+v", mcp)
	}
	if err := m.ToggleResource(mcp, false, true); err != nil {
		t.Fatal(err)
	}
	off := readExtensions(t, agent)
	if strings.Join(off, ",") != "-builtin:mcp" {
		t.Fatalf("off = %v", off)
	}
	rs, err = m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Path == BuiltinPath("mcp") {
			mcp = r
		}
	}
	if mcp.Enabled {
		t.Fatal("mcp still enabled after toggle")
	}
	if err := m.ToggleResource(mcp, false, true); err != nil {
		t.Fatal(err)
	}
	on := readExtensions(t, agent)
	if strings.Join(on, ",") != "+builtin:mcp" {
		t.Fatalf("on = %v", on)
	}
}

func TestToggleBuiltinProjectDoesNotWritePlainPath(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	m, err := Open(cwd, agent, true)
	if err != nil {
		t.Fatal(err)
	}
	m.AutoInstall = false
	rs, err := m.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var mcp Resource
	for _, r := range rs {
		if r.Path == BuiltinPath("mcp") {
			mcp = r
		}
	}
	if err := m.ToggleResource(mcp, true, true); err != nil {
		t.Fatal(err)
	}
	got := readExtensions(t, filepath.Join(cwd, ".pigo"))
	if strings.Join(got, ",") != "-builtin:mcp" {
		t.Fatalf("project = %v", got)
	}
	cfg, err := config.Load(filepath.Join(cwd, ".pigo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Extensions {
		if p == BuiltinPath("mcp") {
			t.Fatal("plain builtin path was written")
		}
	}
}

func writeSettingsJSON(t *testing.T, dir, raw string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readExtensions(t *testing.T, dir string) []string {
	t.Helper()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Extensions
}

func joinBuiltin(rs []Resource) string {
	var names []string
	for _, r := range rs {
		names = append(names, r.Path)
	}
	return strings.Join(names, " ")
}
