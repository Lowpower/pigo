//go:build unix

package tui

import (
	"os"
	"time"

	"github.com/Lowpower/pigo/internal/theme"
	"golang.org/x/term"
)

func readTerminalCellSize(in, out *os.File, timeout time.Duration) (int, int, bool) {
	if in == nil || !term.IsTerminal(int(in.Fd())) {
		return 0, 0, false
	}
	w := out
	if w == nil || !term.IsTerminal(int(w.Fd())) {
		w = in
	}
	if _, err := w.Write([]byte("\x1b[16t")); err != nil {
		return 0, 0, false
	}
	if err := in.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, 0, false
	}
	defer func() { _ = in.SetReadDeadline(time.Time{}) }()
	buf := make([]byte, 0, 256)
	tmp := make([]byte, 256)
	for {
		n, err := in.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if width, height, ok := parseCellSizeResponse(buf); ok {
				return width, height, true
			}
		}
		if err != nil {
			break
		}
	}
	return 0, 0, false
}

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

// readProgramStatus reports whether OSC 7501 reports should be sent.
// env is PIGO_PROGRAM_STATUS: "1" forces them on, "0" forces them off, and
// any other value queries the terminal. A missing TTY cannot answer, so the
// query stays off. PI_PROGRAM_STATUS is not read.
func readProgramStatus(in, out *os.File, timeout time.Duration, env string) bool {
	switch env {
	case "1":
		return true
	case "0":
		return false
	}
	if in == nil || !term.IsTerminal(int(in.Fd())) {
		return false
	}
	w := out
	if w == nil || !term.IsTerminal(int(w.Fd())) {
		w = in
	}
	if _, err := w.Write([]byte(programStatusProbeRequest())); err != nil {
		return false
	}
	if err := in.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return false
	}
	defer func() { _ = in.SetReadDeadline(time.Time{}) }()
	buf := make([]byte, 0, 256)
	tmp := make([]byte, 256)
	supported := false
	for {
		n, err := in.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			var done bool
			supported, done = parseProgramStatusProbe(buf)
			if done {
				return supported
			}
		}
		if err != nil {
			return supported
		}
	}
}
