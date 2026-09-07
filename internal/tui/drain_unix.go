//go:build unix

package tui

import (
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// drainPendingTTY drops unread terminal replies (OSC 11, CSI 6n) so they
// are not delivered as key events after bubbletea takes stdin.
func drainPendingTTY() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags|unix.O_NONBLOCK); err != nil {
		return
	}
	defer unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags) //nolint:errcheck
	buf := make([]byte, 4096)
	for {
		n, err := unix.Read(fd, buf)
		if n <= 0 || err != nil {
			return
		}
	}
}
