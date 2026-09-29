//go:build unix

package tui

import (
	"os"
	"time"

	"github.com/Lowpower/pigo/internal/theme"
	"golang.org/x/term"
)

func readTerminalColors(in, out *os.File, timeout time.Duration) theme.SystemInput {
	if in == nil || !term.IsTerminal(int(in.Fd())) {
		return finishSystemInput(theme.SystemInput{})
	}
	w := out
	if w == nil || !term.IsTerminal(int(w.Fd())) {
		w = in
	}
	if _, err := w.Write([]byte(terminalColorQuery)); err != nil {
		return finishSystemInput(theme.SystemInput{})
	}
	if err := in.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return finishSystemInput(theme.SystemInput{})
	}
	defer func() { _ = in.SetReadDeadline(time.Time{}) }()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	var parsed theme.SystemInput
	done := false
	for !done {
		n, err := in.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			parsed, done = parseColorReplies(buf)
		}
		if err != nil {
			break
		}
	}
	if !done {
		parsed, _ = parseColorReplies(buf)
	}
	return finishSystemInput(parsed)
}
