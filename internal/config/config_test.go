package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("provider: got %q, want %q", cfg.Provider, "anthropic")
	}
	if cfg.Model != "claude-sonnet-4" {
		t.Errorf("model: got %q, want %q", cfg.Model, "claude-sonnet-4")
	}
	if cfg.Theme != "default" {
		t.Errorf("theme: got %q, want %q", cfg.Theme, "default")
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("PIGO_PROVIDER", "openai")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Provider != "openai" {
		t.Errorf("provider: got %q, want %q", cfg.Provider, "openai")
	}
}

func TestLoadSettingsKeyNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"defaultProvider":"openai","defaultModel":"gpt-4o","theme":"light"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ResolvedProvider() != "openai" || cfg.ResolvedModel() != "gpt-4o" || cfg.Theme != "light" {
		t.Fatalf("got provider=%s model=%s theme=%s", cfg.ResolvedProvider(), cfg.ResolvedModel(), cfg.Theme)
	}
}

func TestLoadRetryDefaultsAndOverride(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RetryEnabled() || cfg.RetryMaxRetries() != 3 || cfg.RetryBaseDelayMs() != 2000 {
		t.Fatalf("defaults enabled=%v max=%d delay=%d", cfg.RetryEnabled(), cfg.RetryMaxRetries(), cfg.RetryBaseDelayMs())
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"retry":{"enabled":false,"maxRetries":0,"baseDelayMs":50}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetryEnabled() || cfg.RetryMaxRetries() != 0 || cfg.RetryBaseDelayMs() != 50 {
		t.Fatalf("override enabled=%v max=%d delay=%d", cfg.RetryEnabled(), cfg.RetryMaxRetries(), cfg.RetryBaseDelayMs())
	}
}

func TestLoadThinkingAndIdleTimeout(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPIdleTimeout() != 5*time.Minute {
		t.Fatalf("default idle = %s", cfg.HTTPIdleTimeout())
	}
	dir := t.TempDir()
	raw := `{"thinkingBudgets":{"high":42},"modelThinkingLevels":{"openai/gpt-4o":"low"},"httpIdleTimeoutMs":0}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ThinkingBudgets["high"] != 42 {
		t.Fatalf("budgets=%v", cfg.ThinkingBudgets)
	}
	if cfg.ModelThinkingLevel("openai", "gpt-4o") != "low" {
		t.Fatalf("level=%q", cfg.ModelThinkingLevel("openai", "gpt-4o"))
	}
	if cfg.HTTPIdleTimeout() != 0 {
		t.Fatalf("0 should disable timeout, got %s", cfg.HTTPIdleTimeout())
	}
}

func TestLoadTreeSettings(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DoubleEscape() != "tree" || cfg.TreeFilter() != "default" || cfg.BranchSummarySkipPrompt() {
		t.Fatalf("defaults escape=%s filter=%s skip=%v", cfg.DoubleEscape(), cfg.TreeFilter(), cfg.BranchSummarySkipPrompt())
	}
	dir := t.TempDir()
	raw := `{"doubleEscapeAction":"none","treeFilterMode":"user-only","branchSummary":{"skipPrompt":true,"reserveTokens":100}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DoubleEscape() != "none" || cfg.TreeFilter() != "user-only" || !cfg.BranchSummarySkipPrompt() || cfg.BranchSummaryReserveTokens() != 100 {
		t.Fatalf("got escape=%s filter=%s skip=%v reserve=%d", cfg.DoubleEscape(), cfg.TreeFilter(), cfg.BranchSummarySkipPrompt(), cfg.BranchSummaryReserveTokens())
	}
}

func TestLoadPackagesAndMergeSave(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "defaultProvider": "openai",
  "defaultModel": "gpt-4o",
  "theme": "light",
  "packages": ["npm:@foo/bar", {"source": "git:github.com/a/b", "extensions": ["bin/x"]}],
  "extensions": ["./local.js"],
  "customKeep": true
}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Packages) != 2 || cfg.Packages[0].Source != "npm:@foo/bar" {
		t.Fatalf("packages=%+v", cfg.Packages)
	}
	if !cfg.Packages[1].Filtered() || len(cfg.Packages[1].Extensions) != 1 {
		t.Fatalf("filtered entry=%+v", cfg.Packages[1])
	}
	if len(cfg.Extensions) != 1 {
		t.Fatalf("extensions=%v", cfg.Extensions)
	}
	cfg.Theme = "dark"
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"customKeep"`) {
		t.Fatalf("lost extra key: %s", s)
	}
	if !strings.Contains(s, "npm:@foo/bar") || !strings.Contains(s, `"dark"`) {
		t.Fatalf("missing merged fields: %s", s)
	}
}

func TestSaveDoesNotPersistSessionOnlyModel(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DefaultProvider: "openai", DefaultModel: "gpt-4o", Provider: "anthropic", Model: "claude-haiku-4", Theme: "default"}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DefaultProvider != "openai" || loaded.DefaultModel != "gpt-4o" {
		t.Fatalf("saved default = %s/%s", loaded.DefaultProvider, loaded.DefaultModel)
	}
	if loaded.ResolvedProvider() != "openai" || loaded.ResolvedModel() != "gpt-4o" {
		t.Fatalf("load should restore the saved default as current, got %s/%s", loaded.ResolvedProvider(), loaded.ResolvedModel())
	}
}

func TestExternalEditorCommand(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	cfg := Config{}
	if got := cfg.ExternalEditorCommand(); got != "nano" && got != "notepad" {
		t.Fatalf("default editor = %q", got)
	}
	t.Setenv("EDITOR", "vim")
	if got := cfg.ExternalEditorCommand(); got != "vim" {
		t.Fatalf("EDITOR = %q", got)
	}
	t.Setenv("VISUAL", "emacs")
	if got := cfg.ExternalEditorCommand(); got != "emacs" {
		t.Fatalf("VISUAL = %q", got)
	}
	cfg.ExternalEditor = "code --wait"
	if got := cfg.ExternalEditorCommand(); got != "code --wait" {
		t.Fatalf("settings override = %q", got)
	}
}

func TestLoadTerminalShowImages(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ShowImages() {
		t.Fatal("showImages should default to true")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"terminal":{"showImages":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ShowImages() {
		t.Fatal("showImages=false should stick")
	}
}

func TestLoadBlockImages(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BlockImages() {
		t.Fatal("blockImages should default to false")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"images":{"blockImages":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BlockImages() {
		t.Fatal("blockImages=true should stick")
	}
}

func TestSavePreservesTerminalShowImages(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"terminal":{"showImages":false,"keepMe":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"showImages": false`) {
		t.Fatalf("lost showImages: %s", s)
	}
	if !strings.Contains(s, `"keepMe"`) {
		t.Fatalf("lost extra terminal key: %s", s)
	}
}

func TestLoadMarkdownMermaid(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MermaidMode() != "streaming" {
		t.Fatalf("default mermaid = %q", cfg.MermaidMode())
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"markdown":{"mermaid":"off"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MermaidMode() != "off" {
		t.Fatalf("loaded %q", cfg.MermaidMode())
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"markdown":{"mermaid":"sometimes"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MermaidMode() != "streaming" {
		t.Fatalf("invalid should fall back, got %q", cfg.MermaidMode())
	}
}

func TestSavePreservesMarkdownMermaid(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"markdown":{"mermaid":"final","keepMe":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"mermaid": "final"`) {
		t.Fatalf("lost mermaid: %s", s)
	}
	if !strings.Contains(s, `"keepMe"`) {
		t.Fatalf("lost extra markdown key: %s", s)
	}
}

func TestLoadTuiMode(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TuiMode() != "regular" || cfg.FullscreenExit() != "transcript" {
		t.Fatalf("defaults mode=%s exit=%s", cfg.TuiMode(), cfg.FullscreenExit())
	}
	dir := t.TempDir()
	raw := `{"tuiMode":"fullscreen","fullscreenExitOutput":"resume-hint"}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TuiMode() != "fullscreen" || cfg.FullscreenExit() != "resume-hint" {
		t.Fatalf("got mode=%s exit=%s", cfg.TuiMode(), cfg.FullscreenExit())
	}
}

func TestLoadAndSaveExternalEditor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"externalEditor":"hx"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExternalEditor != "hx" {
		t.Fatalf("loaded %q", cfg.ExternalEditor)
	}
	cfg.ExternalEditor = "nvim"
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"externalEditor": "nvim"`) {
		t.Fatalf("saved: %s", b)
	}
}

func TestChangelogSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"lastChangelogVersion":"0.0.0","collapseChangelog":true,"enableInstallTelemetry":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastChangelogVersion != "0.0.0" || !cfg.CollapsedChangelog() || cfg.InstallTelemetryEnabled() {
		t.Fatalf("got last=%q collapse=%v tel=%v", cfg.LastChangelogVersion, cfg.CollapsedChangelog(), cfg.InstallTelemetryEnabled())
	}
	fresh, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if fresh.LastChangelogVersion != "" || fresh.CollapsedChangelog() || !fresh.InstallTelemetryEnabled() {
		t.Fatalf("defaults last=%q collapse=%v tel=%v", fresh.LastChangelogVersion, fresh.CollapsedChangelog(), fresh.InstallTelemetryEnabled())
	}
}

func TestDefaultToolsLoadAndSave(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.InitialBuiltinTools(); strings.Join(got, ",") != "read,bash,edit,write" {
		t.Fatalf("unset defaultTools = %v", got)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"defaultTools":["grep","find"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.InitialBuiltinTools(); strings.Join(got, ",") != "grep,find" {
		t.Fatalf("loaded = %v", got)
	}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"defaultTools"`) || !strings.Contains(string(b), `"grep"`) {
		t.Fatalf("saved: %s", b)
	}
	emptyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(emptyDir, "settings.json"), []byte(`{"defaultTools":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(emptyDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultTools == nil || len(*cfg.DefaultTools) != 0 {
		t.Fatalf("empty list should be set, got %v", cfg.DefaultTools)
	}
	if n := len(cfg.InitialBuiltinTools()); n != 0 {
		t.Fatalf("empty defaultTools should yield no builtins, n=%d", n)
	}
}

func TestLoadNestedCompactionAndThinkingAlias(t *testing.T) {
	dir := t.TempDir()
	raw := `{"compaction":{"enabled":false,"reserveTokens":1,"keepRecentTokens":2},"defaultThinkingLevel":"high","sessionDir":"/tmp/sessions","quietStartup":true,"httpProxy":"http://127.0.0.1:9","enableSkillCommands":false,"enabledModels":["openai/*"]}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CompactionEnabled() || cfg.ReserveTokens != 1 || cfg.KeepRecentTokens != 2 {
		t.Fatalf("compaction enabled=%v reserve=%d keep=%d", cfg.CompactionEnabled(), cfg.ReserveTokens, cfg.KeepRecentTokens)
	}
	if cfg.Thinking != "high" {
		t.Fatalf("thinking=%q", cfg.Thinking)
	}
	if cfg.SessionDir != "/tmp/sessions" || !cfg.QuietStartup() || cfg.HTTPProxy != "http://127.0.0.1:9" {
		t.Fatalf("sessionDir=%q quiet=%v proxy=%q", cfg.SessionDir, cfg.QuietStartup(), cfg.HTTPProxy)
	}
	if cfg.SkillCommandsEnabled() {
		t.Fatal("enableSkillCommands=false")
	}
	if len(cfg.EnabledModels) != 1 || cfg.EnabledModels[0] != "openai/*" {
		t.Fatalf("enabledModels=%v", cfg.EnabledModels)
	}
}

func TestApplyProjectOverlayAndUntrusted(t *testing.T) {
	agent := t.TempDir()
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte(`{"theme":"dark","thinking":"low","keepMe":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, ".pigo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "settings.json"), []byte(`{"theme":"light","compaction":{"enabled":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	base, err := Load(agent)
	if err != nil {
		t.Fatal(err)
	}
	trusted := ApplyProject(base, cwd, true)
	if trusted.Theme != "light" || trusted.CompactionEnabled() {
		t.Fatalf("trusted theme=%s compact=%v", trusted.Theme, trusted.CompactionEnabled())
	}
	untrusted := ApplyProject(base, cwd, false)
	if untrusted.Theme != "dark" || !untrusted.CompactionEnabled() {
		t.Fatalf("untrusted theme=%s compact=%v", untrusted.Theme, untrusted.CompactionEnabled())
	}
}

func TestSaveWritesNestedCompactionAndKeepsUnknown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"keepMe":true,"compactionEnabled":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	off := false
	cfg.CompactionOn = &off
	cfg.ReserveTokens = 3
	cfg.KeepRecentTokens = 4
	cfg.SessionDir = "/s"
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"keepMe"`) || !strings.Contains(s, `"compaction"`) || !strings.Contains(s, `"sessionDir"`) {
		t.Fatalf("saved: %s", s)
	}
}

func TestEnabledModelsThreeStates(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnabledModels != nil {
		t.Fatalf("missing file: %v", cfg.EnabledModels)
	}

	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"enabledModels":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnabledModels == nil || len(cfg.EnabledModels) != 0 {
		t.Fatalf("empty array: %#v", cfg.EnabledModels)
	}

	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"enabledModels":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnabledModels != nil {
		t.Fatalf("null: %#v", cfg.EnabledModels)
	}
	cfg.Theme = "dark"
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"enabledModels"`) {
		t.Fatalf("saving unrelated change rewrote null enabledModels: %s", b)
	}

	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"enabledModels":["anthropic/claude-sonnet-4"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.EnabledModels) != 1 || cfg.EnabledModels[0] != "anthropic/claude-sonnet-4" {
		t.Fatalf("list: %#v", cfg.EnabledModels)
	}
}

func TestSaveEnabledModelsDeleteAndKeep(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DefaultProvider: "openai", DefaultModel: "gpt-4o", Theme: "default", EnabledModels: []string{"openai/gpt-4o"}}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"enabledModels"`) || !strings.Contains(string(b), "openai/gpt-4o") {
		t.Fatalf("write: %s", b)
	}
	cfg.Theme = "dark"
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"enabledModels"`) || !strings.Contains(string(b), `"dark"`) {
		t.Fatalf("preserve: %s", b)
	}
	cfg.EnabledModels = nil
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "enabledModels") {
		t.Fatalf("delete: %s", b)
	}
}

func TestSaveEnabledModelsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Theme: "default", EnabledModels: []string{}}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"enabledModels"`) {
		t.Fatalf("empty array should write the key: %s", b)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EnabledModels == nil {
		t.Fatal("empty array loaded as nil")
	}
}
