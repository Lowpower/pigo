package changelog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/version"
)

func TestParseAndNewEntries(t *testing.T) {
	entries := Parse(embedded)
	if len(entries) == 0 {
		t.Fatal("expected embedded changelog entries")
	}
	if entries[0].Version() != "0.0.1" {
		t.Fatalf("first=%s", entries[0].Version())
	}
	if len(NewEntries(entries, "0.0.1-dev")) != 0 {
		t.Fatal("0.0.1-dev should equal 0.0.1")
	}
	if len(NewEntries(entries, "0.0.0")) == 0 {
		t.Fatal("0.0.0 should see 0.0.1 as new")
	}
}

func TestNormalizeLinks(t *testing.T) {
	in := "See [export](internal/session/export_html.go) and [abs](/README.md)."
	out := NormalizeLinks(in, "0.0.1")
	if !strings.Contains(out, "https://github.com/Lowpower/pigo/blob/v0.0.1/internal/session/export_html.go") {
		t.Fatalf("relative: %s", out)
	}
	if !strings.Contains(out, "https://github.com/Lowpower/pigo/blob/v0.0.1/README.md") {
		t.Fatalf("abs: %s", out)
	}
}

func TestStartupNoticeFirstInstall(t *testing.T) {
	t.Setenv("PIGO_OFFLINE", "1")
	dir := t.TempDir()
	cfg := config.Config{Theme: "default"}
	if got := StartupNotice(&cfg, dir); got != "" {
		t.Fatalf("first install showed %q", got)
	}
	if cfg.LastChangelogVersion != version.Version {
		t.Fatalf("recorded %q", cfg.LastChangelogVersion)
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastChangelogVersion != version.Version {
		t.Fatalf("saved %q", loaded.LastChangelogVersion)
	}
}

func TestStartupNoticeUpgrade(t *testing.T) {
	t.Setenv("PIGO_OFFLINE", "1")
	dir := t.TempDir()
	cfg := config.Config{Theme: "default", LastChangelogVersion: "0.0.0"}
	got := StartupNotice(&cfg, dir)
	if !strings.Contains(got, "What's New") && !strings.Contains(got, "0.0.1") {
		t.Fatalf("upgrade notice=%q", got)
	}
	if cfg.LastChangelogVersion != version.Version {
		t.Fatalf("updated last=%q", cfg.LastChangelogVersion)
	}
}

func TestStartupNoticeCollapsed(t *testing.T) {
	t.Setenv("PIGO_OFFLINE", "1")
	dir := t.TempDir()
	tru := true
	cfg := config.Config{Theme: "default", LastChangelogVersion: "0.0.0", CollapseChangelog: &tru}
	got := StartupNotice(&cfg, dir)
	if !strings.Contains(got, "Updated to v") || !strings.Contains(got, "/changelog") {
		t.Fatalf("collapsed=%q", got)
	}
}

func TestFullMarkdown(t *testing.T) {
	got := FullMarkdown()
	if !strings.Contains(got, "## 0.0.1") {
		t.Fatalf("%s", got)
	}
}

func TestTelemetryEnvOverridesSettings(t *testing.T) {
	on := true
	off := false
	cfgOn := config.Config{EnableInstallTelemetry: &on}
	cfgOff := config.Config{EnableInstallTelemetry: &off}

	t.Setenv("PI_TELEMETRY", "0")
	os.Unsetenv("PI_TELEMETRY")
	os.Unsetenv("PIGO_TELEMETRY")
	if !TelemetryAllowed(cfgOn) {
		t.Fatal("settings on should allow telemetry")
	}
	if TelemetryAllowed(cfgOff) {
		t.Fatal("settings off should deny telemetry")
	}

	t.Setenv("PI_TELEMETRY", "0")
	if TelemetryAllowed(cfgOn) {
		t.Fatal("PI_TELEMETRY=0 should deny even when settings enable it")
	}
	t.Setenv("PI_TELEMETRY", "true")
	if !TelemetryAllowed(cfgOff) {
		t.Fatal("PI_TELEMETRY=true should allow even when settings disable it")
	}
	t.Setenv("PIGO_TELEMETRY", "no")
	if TelemetryAllowed(cfgOn) {
		t.Fatal("PIGO_TELEMETRY should win over PI_TELEMETRY")
	}
}

func TestEmbeddedMatchesRepoChangelog(t *testing.T) {
	root, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(root) != embedded {
		t.Fatal("internal/changelog/changelog.md is out of sync with CHANGELOG.md")
	}
}
