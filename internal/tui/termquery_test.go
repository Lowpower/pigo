package tui

import (
	"testing"

	"github.com/Lowpower/pigo/internal/theme"
)

func TestParseLightBackgroundReply(t *testing.T) {
	raw := "\x1b]11;rgb:ffff/ffff/ffff\x07\x1b[?6c"
	in, done := parseColorReplies([]byte(raw))
	if !done || in.Background == nil || in.Background.R != 255 || in.Background.G != 255 || in.Background.B != 255 {
		t.Fatalf("done=%v in=%+v", done, in)
	}
}

func TestParsePartialReplyIsIncomplete(t *testing.T) {
	_, done := parseColorReplies([]byte("\x1b]11;rgb:0000/0000/0000\x07"))
	if done {
		t.Fatal("OSC 11 alone should not finish the query")
	}
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

func TestParseDA1FinishesWithoutColors(t *testing.T) {
	in, done := parseColorReplies([]byte("\x1b[?1;2c"))
	if !done || in.Background != nil || in.Palette != nil {
		t.Fatalf("done=%v in=%+v", done, in)
	}
}
