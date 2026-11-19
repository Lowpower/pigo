package tui

import (
	"strings"
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

func TestTerminalColorQueryRequestsScheme(t *testing.T) {
	if !strings.HasPrefix(terminalColorQuery, "\x1b[?2031h") {
		t.Fatalf("query = %q", terminalColorQuery)
	}
	if !strings.Contains(terminalColorQuery, "\x1b]11;?") || !strings.HasSuffix(terminalColorQuery, "\x1b[c") {
		t.Fatalf("query = %q", terminalColorQuery)
	}
	if terminalColorSchemeDisable != "\x1b[?2031l" {
		t.Fatalf("disable = %q", terminalColorSchemeDisable)
	}
}

func TestParseColorSchemeReport(t *testing.T) {
	t.Run("light report does not finish the query", func(t *testing.T) {
		in, done := parseColorReplies([]byte("\x1b[?997;2n"))
		if done {
			t.Fatal("scheme report should not finish the color query")
		}
		if in.ReportedAppearance != "light" {
			t.Fatalf("reported = %q", in.ReportedAppearance)
		}
	})
	t.Run("dark report", func(t *testing.T) {
		in, done := parseColorReplies([]byte("\x1b[?997;1n"))
		if done || in.ReportedAppearance != "dark" {
			t.Fatalf("done=%v reported=%q", done, in.ReportedAppearance)
		}
	})
	t.Run("between OSC and DA1", func(t *testing.T) {
		partial := []byte("\x1b]11;rgb:ffff/ffff/ffff\x07\x1b[?997;1n")
		in, done := parseColorReplies(partial)
		if done {
			t.Fatal("scheme report between OSC and DA1 should not finish the query")
		}
		if in.Background == nil || in.ReportedAppearance != "dark" {
			t.Fatalf("%+v", in)
		}
		in, done = parseColorReplies(append(partial, []byte("\x1b[?6c")...))
		if !done || in.Background == nil || in.ReportedAppearance != "dark" {
			t.Fatalf("done=%v in=%+v", done, in)
		}
	})
	t.Run("last report wins", func(t *testing.T) {
		in, _ := parseColorReplies([]byte("\x1b[?997;1n\x1b[?997;2n"))
		if in.ReportedAppearance != "light" {
			t.Fatalf("reported = %q", in.ReportedAppearance)
		}
	})
	t.Run("report after DA1", func(t *testing.T) {
		in, done := parseColorReplies([]byte("\x1b[?6c\x1b[?997;2n"))
		if !done || in.Background != nil || in.ReportedAppearance != "light" {
			t.Fatalf("done=%v in=%+v", done, in)
		}
	})
}

func TestFinishSystemInputPriority(t *testing.T) {
	t.Run("report without COLORFGBG", func(t *testing.T) {
		t.Setenv("COLORFGBG", "")
		in := finishSystemInput(theme.SystemInput{ReportedAppearance: "light"})
		if in.AppearanceHint != "light" {
			t.Fatalf("hint = %q", in.AppearanceHint)
		}
		in = finishSystemInput(theme.SystemInput{ReportedAppearance: "dark"})
		if in.AppearanceHint != "dark" {
			t.Fatalf("hint = %q", in.AppearanceHint)
		}
	})
	t.Run("report beats COLORFGBG", func(t *testing.T) {
		t.Setenv("COLORFGBG", "15;0")
		in := finishSystemInput(theme.SystemInput{ReportedAppearance: "light"})
		if in.AppearanceHint != "light" {
			t.Fatalf("hint = %q", in.AppearanceHint)
		}
	})
	t.Run("background ignores report", func(t *testing.T) {
		t.Setenv("COLORFGBG", "15;0")
		bg := theme.RGB{R: 255, G: 255, B: 255}
		in := finishSystemInput(theme.SystemInput{
			Background:         &bg,
			ReportedAppearance: "dark",
		})
		if in.AppearanceHint != "" {
			t.Fatalf("hint = %q", in.AppearanceHint)
		}
		th := theme.System(in)
		if th.Appearance != "light" {
			t.Fatalf("appearance = %q", th.Appearance)
		}
	})
	t.Run("none is dark", func(t *testing.T) {
		t.Setenv("COLORFGBG", "")
		in := finishSystemInput(theme.SystemInput{})
		if in.AppearanceHint != "dark" {
			t.Fatalf("hint = %q", in.AppearanceHint)
		}
	})
}

func TestColorQueryStop(t *testing.T) {
	bg := theme.RGB{}
	if colorQueryStop(theme.SystemInput{}, false) {
		t.Fatal("unfinished query should keep reading")
	}
	if colorQueryStop(theme.SystemInput{ReportedAppearance: "light"}, false) {
		t.Fatal("scheme report should not end the color query")
	}
	if !colorQueryStop(theme.SystemInput{Background: &bg}, true) {
		t.Fatal("background should stop after the color query")
	}
	if !colorQueryStop(theme.SystemInput{ReportedAppearance: "dark"}, true) {
		t.Fatal("scheme should stop after the color query")
	}
	if colorQueryStop(theme.SystemInput{}, true) {
		t.Fatal("no background and no scheme should keep waiting")
	}
}
