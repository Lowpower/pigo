package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRuntimeSettingDefaults(t *testing.T) {
	var c Config
	if !c.AutoResize() {
		t.Fatal("autoResize default true")
	}
	if c.ImageWidthCells() != 60 {
		t.Fatalf("width=%d", c.ImageWidthCells())
	}
	if !c.HyperlinksEnabled(true) || c.HyperlinksEnabled(false) {
		t.Fatal("hyperlinks auto follows tty")
	}
	if c.CodeBlockIndent() != "  " {
		t.Fatalf("indent=%q", c.CodeBlockIndent())
	}
	if c.HideThinking() || c.CacheMissNotices() {
		t.Fatal("thinking/cache notices default false")
	}
	if c.AutocompleteVisible() != 5 {
		t.Fatalf("visible=%d", c.AutocompleteVisible())
	}
	if c.ProviderRetryMaxRetries() != 0 {
		t.Fatal("provider retries default 0")
	}
	if c.ProviderRetryMaxDelay() != 60*time.Second {
		t.Fatalf("delay=%s", c.ProviderRetryMaxDelay())
	}
	if c.EditorPadX() != 0 || c.OutputPadN() != 0 || c.HardwareCursor() || c.ClearOnShrink() || c.TerminalProgress() {
		t.Fatal("tui extras default off")
	}
	if c.TrueColorMode() != "auto" {
		t.Fatalf("trueColor=%s", c.TrueColorMode())
	}
}

func TestRuntimeSettingOverrides(t *testing.T) {
	w, r, vis := 80, 4, 12
	off, on := false, true
	c := Config{
		Images:                 ImageSettings{AutoResize: &off},
		Terminal:               TerminalSettings{ImageWidthCells: &w, Hyperlinks: false, Images: "kitty"},
		Markdown:               MarkdownSettings{CodeBlockIndent: "\t"},
		HideThinkingBlock:      &on,
		ShowCacheMissNotices:   &on,
		ShellCommandPrefix:     "shopt -s expand_aliases",
		AutocompleteMaxVisible: &vis,
		Retry: RetrySettings{Provider: &ProviderRetrySettings{
			TimeoutMs: intPtr(1000), MaxRetries: &r, MaxRetryDelayMs: intPtr(5000),
		}},
	}
	if c.AutoResize() {
		t.Fatal("autoResize off")
	}
	if c.ImageWidthCells() != 80 {
		t.Fatalf("width=%d", c.ImageWidthCells())
	}
	if c.HyperlinksEnabled(true) {
		t.Fatal("hyperlinks false")
	}
	if c.CodeBlockIndent() != "\t" {
		t.Fatalf("indent=%q", c.CodeBlockIndent())
	}
	if !c.HideThinking() || !c.CacheMissNotices() {
		t.Fatal("flags")
	}
	if c.ShellPrefix() != "shopt -s expand_aliases" {
		t.Fatalf("prefix=%q", c.ShellPrefix())
	}
	if c.AutocompleteVisible() != 12 {
		t.Fatalf("visible=%d", c.AutocompleteVisible())
	}
	if c.ImageProtocol("iterm2") != "kitty" {
		t.Fatalf("proto=%q", c.ImageProtocol("iterm2"))
	}
	if c.ProviderRetryMaxRetries() != 4 || c.ProviderRetryTimeout() != time.Second {
		t.Fatal("provider retry")
	}
	if c.StreamIdleTimeout() != time.Second {
		t.Fatal("stream idle should use provider timeout")
	}
}

func TestLoadSaveExtraRuntimeKeys(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "hideThinkingBlock": true,
  "showCacheMissNotices": true,
  "shellCommandPrefix": "set -e",
  "autocompleteMaxVisible": 8,
  "editorPaddingX": 2,
  "outputPad": 1,
  "showHardwareCursor": true,
  "websocketConnectTimeoutMs": 4000,
  "fullscreenScrollbar": true,
  "fullscreenCopyOnSelect": false,
  "enableAnalytics": false,
  "trackingId": "abc",
  "transport": "sse",
  "warnings": {"foo": false},
  "terminal": {"clearOnShrink": true, "showTerminalProgress": true, "images": "auto", "trueColor": true}
}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HideThinking() || !cfg.CacheMissNotices() || cfg.ShellPrefix() != "set -e" {
		t.Fatalf("cfg=%+v", cfg)
	}
	if cfg.AutocompleteVisible() != 8 {
		t.Fatalf("visible=%d", cfg.AutocompleteVisible())
	}
	if cfg.EditorPaddingX == nil || *cfg.EditorPaddingX != 2 {
		t.Fatalf("pad=%v", cfg.EditorPaddingX)
	}
	if cfg.Transport != "sse" || cfg.TrackingID != "abc" {
		t.Fatalf("transport=%q id=%q", cfg.Transport, cfg.TrackingID)
	}
	if cfg.Terminal.ClearOnShrink == nil || !*cfg.Terminal.ClearOnShrink {
		t.Fatal("clearOnShrink")
	}
	if cfg.TrueColorMode() != "on" || !cfg.ClearOnShrink() || !cfg.TerminalProgress() {
		t.Fatal("terminal extras")
	}
	if cfg.EditorPadX() != 2 || cfg.OutputPadN() != 1 || !cfg.HardwareCursor() {
		t.Fatal("editor extras")
	}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"hideThinkingBlock"`, `"showCacheMissNotices"`, `"shellCommandPrefix"`, `"autocompleteMaxVisible"`, `"clearOnShrink"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
}

func intPtr(v int) *int { return &v }

var trackingIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestSetEnableAnalyticsMintsTrackingID(t *testing.T) {
	var c Config
	c.SetEnableAnalytics(true)
	if c.EnableAnalytics == nil || !*c.EnableAnalytics {
		t.Fatal("expected analytics on")
	}
	if !trackingIDRe.MatchString(c.TrackingID) {
		t.Fatalf("trackingId=%q", c.TrackingID)
	}
	first := c.TrackingID
	c.SetEnableAnalytics(false)
	if c.EnableAnalytics == nil || *c.EnableAnalytics {
		t.Fatal("expected analytics off")
	}
	if c.TrackingID != first {
		t.Fatalf("id cleared: %q", c.TrackingID)
	}
	c.SetEnableAnalytics(true)
	if c.TrackingID != first {
		t.Fatalf("id changed on re-enable: %q vs %q", c.TrackingID, first)
	}
}

func TestSaveMintsTrackingIDWhenAnalyticsOn(t *testing.T) {
	dir := t.TempDir()
	on := true
	if err := Save(dir, Config{EnableAnalytics: &on}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.EnableAnalytics == nil || !*got.EnableAnalytics {
		t.Fatal("expected analytics on")
	}
	if !trackingIDRe.MatchString(got.TrackingID) {
		t.Fatalf("trackingId=%q", got.TrackingID)
	}
}
