package session

import (
	"strings"
	"testing"
)

func TestAnsiToHTML(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want []string
	}{
		{"colors and escape", ansiToHTML("\x1b[1;31mred\x1b[0m <tag>"), []string{`font-weight:bold`, `color:#800000`, `red`, `&lt;tag&gt;`}},
		{"256 color", ansiToHTML("\x1b[38;5;196mhi\x1b[0m"), []string{`color:#ff0000`, "hi"}},
		{"rgb", ansiToHTML("\x1b[38;2;10;20;30mrgb\x1b[0m"), []string{`color:rgb(10,20,30)`}},
		{"lines", ansiLinesToHTML([]string{"\x1b[32mok\x1b[0m", ""}), []string{`class="ansi-line"`, `ok`, `&nbsp;`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, want := range tt.want {
				if !strings.Contains(tt.got, want) {
					t.Fatalf("missing %q in %q", want, tt.got)
				}
			}
		})
	}
}
