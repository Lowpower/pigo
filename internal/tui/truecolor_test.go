package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/theme"
)

func restoreColorProfile(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func clearTrueColorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PIGO_TRUE_COLOR", "")
	t.Setenv("PI_TRUE_COLOR", "")
}

func TestCustomThemeTrueColorSequences(t *testing.T) {
	clearTrueColorEnv(t)
	restoreColorProfile(t)

	cfg := testCfg()
	cfg.Terminal.TrueColor = "off"
	applyTrueColor(cfg)
	if lipgloss.ColorProfile() != termenv.ANSI256 {
		t.Fatalf("off profile = %v", lipgloss.ColorProfile())
	}
	off := styleFG(lipgloss.NewStyle(), "#3c3544").Render("x")
	if strings.Contains(off, "38;2;") || !strings.Contains(off, "38;5;") {
		t.Fatalf("off = %q", off)
	}

	cfg.Terminal.TrueColor = "on"
	applyTrueColor(cfg)
	if lipgloss.ColorProfile() != termenv.TrueColor {
		t.Fatalf("on profile = %v", lipgloss.ColorProfile())
	}
	on := styleFG(lipgloss.NewStyle(), "#3c3544").Render("x")
	if !strings.Contains(on, "38;2;") || strings.Contains(on, "38;5;") {
		t.Fatalf("on = %q", on)
	}

	cfg.Terminal.TrueColor = nil
	applyTrueColor(cfg)
	want := colorProfile("auto", termenv.EnvColorProfile())
	if lipgloss.ColorProfile() != want {
		t.Fatalf("auto profile = %v, want %v", lipgloss.ColorProfile(), want)
	}
}

func TestColorProfileFollowsDetection(t *testing.T) {
	if colorProfile("on", termenv.ANSI256) != termenv.TrueColor {
		t.Fatal("on")
	}
	if colorProfile("off", termenv.TrueColor) != termenv.ANSI256 {
		t.Fatal("off")
	}
	if colorProfile("auto", termenv.TrueColor) != termenv.TrueColor {
		t.Fatal("auto truecolor")
	}
	for _, detected := range []termenv.Profile{termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		if colorProfile("auto", detected) != termenv.ANSI256 {
			t.Fatalf("auto %v", detected)
		}
	}
}

func TestBuiltinThemesRenderInBothColorModes(t *testing.T) {
	clearTrueColorEnv(t)
	restoreColorProfile(t)

	for _, name := range []string{"dark", "light"} {
		th := theme.Load(name, "", "")
		if th.Accent == "" {
			t.Fatalf("%s accent empty", name)
		}
		for _, mode := range []string{"on", "off"} {
			cfg := testCfg()
			cfg.Terminal.TrueColor = mode
			applyTrueColor(cfg)
			out := styleFG(lipgloss.NewStyle(), th.Accent).Render("x")
			if out == "" || out == "x" || !strings.Contains(out, "\x1b[") {
				t.Fatalf("%s %s = %q", name, mode, out)
			}
		}
	}
}

func TestReloadReappliesTrueColor(t *testing.T) {
	clearTrueColorEnv(t)
	restoreColorProfile(t)

	agent := t.TempDir()
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte(`{"terminal":{"trueColor":"off"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := runtime.New(context.Background(), runtime.Options{
		AgentDir:     agent,
		Cwd:          t.TempDir(),
		Offline:      true,
		NoTools:      true,
		NoExtensions: true,
		Config:       config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Theme: "dark"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })

	cfg := testCfg()
	cfg.Terminal.TrueColor = "on"
	m := New(cfg)
	m.engine = e
	if got := styleFG(lipgloss.NewStyle(), "#3c3544").Render("x"); !strings.Contains(got, "38;2;") {
		t.Fatalf("before reload = %q", got)
	}
	m = submit(m, "/reload")
	got := styleFG(lipgloss.NewStyle(), "#3c3544").Render("x")
	if strings.Contains(got, "38;2;") || !strings.Contains(got, "38;5;") {
		t.Fatalf("after reload = %q", got)
	}
}
