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

func TestHideThinkingFromSettings(t *testing.T) {
	on := true
	m := New(config.Config{HideThinkingBlock: &on, Theme: "default"})
	if !m.hideThinking {
		t.Fatal("should start hidden")
	}
}
