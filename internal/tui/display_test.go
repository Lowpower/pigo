package tui

import (
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
)

func TestOSC8FileLink(t *testing.T) {
	m := New(testCfg())
	m.cfg.Terminal.Hyperlinks = true
	got := m.linkPath("a.go", "/tmp/a.go")
	if !strings.Contains(got, "\x1b]8;;file://") || !strings.Contains(got, "/tmp/a.go") {
		t.Fatalf("%q", got)
	}
	if !strings.Contains(got, "a.go") {
		t.Fatalf("missing label: %q", got)
	}
}

func TestHyperlinksOffSkipsOSC8(t *testing.T) {
	m := New(testCfg())
	m.cfg.Terminal.Hyperlinks = false
	got := m.linkPath("a.go", "/tmp/a.go")
	if strings.Contains(got, "\x1b]8;;") {
		t.Fatalf("%q", got)
	}
	if got != "a.go" {
		t.Fatalf("%q", got)
	}
}

func TestIndentMarkdownCodeBlocks(t *testing.T) {
	src := "hello\n```\nfoo\n```\n"
	got := indentMarkdownCodeBlocks(src, "\t")
	if !strings.Contains(got, "\tfoo") {
		t.Fatalf("%q", got)
	}
}

func TestFormatToolCallHyperlink(t *testing.T) {
	m := New(testCfg())
	m.cfg.Terminal.Hyperlinks = true
	got := formatToolCall("read", map[string]any{"path": "/tmp/a.go"}, m.linkPath)
	if !strings.Contains(got, "\x1b]8;;file://") || !strings.Contains(got, "/tmp/a.go") {
		t.Fatalf("%q", got)
	}
}

func TestPadLinesAndProgressOSC(t *testing.T) {
	if got := padLines("hi\n", 2); got != "  hi\n" {
		t.Fatalf("pad=%q", got)
	}
	if !strings.Contains(progressOSC(true), "9;4;1") {
		t.Fatal("running progress")
	}
	if !strings.Contains(progressOSC(false), "9;4;0") {
		t.Fatal("idle progress")
	}
	clipped := clipWithScrollbar(strings.Repeat("x\n", 10), 3)
	if strings.Count(clipped, "\n") != 2 {
		t.Fatalf("clipped lines=%q", clipped)
	}
	if !strings.Contains(clipped, "▐") {
		t.Fatalf("missing thumb: %q", clipped)
	}
}

func TestOutputPadAppliesToView(t *testing.T) {
	n := 2
	m := New(config.Config{Theme: "default", OutputPad: &n})
	got := m.present("hello")
	if !strings.HasPrefix(got, "  hello") {
		t.Fatalf("%q", got)
	}
}

func TestHideThinkingFromSettings(t *testing.T) {
	on := true
	m := New(config.Config{HideThinkingBlock: &on, Theme: "default"})
	if !m.hideThinking {
		t.Fatal("should start hidden")
	}
}
