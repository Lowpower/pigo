package session

import (
	"strings"
	"testing"
)

func TestAnsiToHTMLColorsAndEscape(t *testing.T) {
	in := "\x1b[1;31mred\x1b[0m <tag>"
	got := ansiToHTML(in)
	for _, want := range []string{`font-weight:bold`, `color:#800000`, `red`, `&lt;tag&gt;`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestAnsiLinesToHTML(t *testing.T) {
	got := ansiLinesToHTML([]string{"\x1b[32mok\x1b[0m", ""})
	for _, want := range []string{`class="ansi-line"`, `ok`, `&nbsp;`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestAnsi256AndRGB(t *testing.T) {
	got := ansiToHTML("\x1b[38;5;196mhi\x1b[0m")
	if !strings.Contains(got, `color:#ff0000`) || !strings.Contains(got, "hi") {
		t.Fatalf("256: %q", got)
	}
	got = ansiToHTML("\x1b[38;2;10;20;30mrgb\x1b[0m")
	if !strings.Contains(got, `color:rgb(10,20,30)`) {
		t.Fatalf("rgb: %q", got)
	}
}
