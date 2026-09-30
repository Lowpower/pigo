package tui

import (
	"testing"

	"github.com/Lowpower/pigo/internal/theme"
)

func TestParseColorReplies(t *testing.T) {
	t.Run("light background", func(t *testing.T) {
		in, done := parseColorReplies([]byte("\x1b]11;rgb:ffff/ffff/ffff\x07\x1b[?6c"))
		if !done || in.Background == nil || in.Background.R != 255 || in.Background.G != 255 || in.Background.B != 255 {
			t.Fatalf("done=%v in=%+v", done, in)
		}
	})
	t.Run("partial reply is incomplete", func(t *testing.T) {
		if _, done := parseColorReplies([]byte("\x1b]11;rgb:0000/0000/0000\x07")); done {
			t.Fatal("OSC 11 alone should not finish the query")
		}
	})
	t.Run("DA1 finishes without colors", func(t *testing.T) {
		in, done := parseColorReplies([]byte("\x1b[?1;2c"))
		if !done || in.Background != nil || in.Palette != nil {
			t.Fatalf("done=%v in=%+v", done, in)
		}
	})
}

func TestFinishSystemInputUsesColorFgBg(t *testing.T) {
	t.Setenv("COLORFGBG", "15;0")
	in := finishSystemInput(theme.SystemInput{})
	if in.AppearanceHint != "dark" {
		t.Fatalf("hint = %q", in.AppearanceHint)
	}
	bg := theme.RGB{R: 255, G: 255, B: 255}
	in = finishSystemInput(theme.SystemInput{Background: &bg})
	if in.AppearanceHint != "" {
		t.Fatalf("background should ignore COLORFGBG, hint=%q", in.AppearanceHint)
	}
}
