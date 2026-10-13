package tui

import (
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	escDelayLocal = 10 * time.Millisecond
	escDelaySSH   = 100 * time.Millisecond
)

// escDelayReader joins a lone trailing ESC with bytes that arrive within
// timeout, so a split Alt+Enter (ESC, then CR) is one Read for Bubble Tea.
// A full buffer is returned as-is: Bubble Tea already treats that as an
// incomplete tail. Timeout 0 disables the wait.
//
// Fd, Name, and the file methods stay on the underlying *os.File so Bubble
// Tea can enter raw mode and cancel a blocked read. The wait uses poll, not
// SetReadDeadline: Bubble Tea calls Fd(), and that puts the descriptor into
// blocking mode, after which deadlines no longer fire.
type escDelayReader struct {
	f       *os.File
	timeout time.Duration
	pending []byte
	pendErr error
	// late is a Read still blocked after a timed-out wait (Windows).
	// The next fill takes that result instead of reading the file again.
	late chan delayedRead
}

type delayedRead struct {
	b   []byte
	err error
}

func newEscDelayReader(f *os.File, timeout time.Duration) *escDelayReader {
	return &escDelayReader{f: f, timeout: timeout}
}

// programInput is stdin with the ESC hold from PIGO_TUI_ESC_TIMEOUT / PI_TUI_ESC_TIMEOUT.
func programInput() *escDelayReader {
	return newEscDelayReader(os.Stdin, escDelayTimeout(os.Getenv))
}

// escDelayTimeout resolves the lone-ESC wait. PIGO_TUI_ESC_TIMEOUT wins over
// PI_TUI_ESC_TIMEOUT. A parsed value >= 0 is used, and 0 disables the wait.
// Negative or non-numeric values are ignored. Otherwise SSH is 100ms and a
// local terminal is 10ms.
func escDelayTimeout(getenv func(string) string) time.Duration {
	if d, ok := parseEscDelay(getenv("PIGO_TUI_ESC_TIMEOUT")); ok {
		return d
	}
	if d, ok := parseEscDelay(getenv("PI_TUI_ESC_TIMEOUT")); ok {
		return d
	}
	if strings.TrimSpace(getenv("SSH_CONNECTION")) != "" || strings.TrimSpace(getenv("SSH_TTY")) != "" {
		return escDelaySSH
	}
	return escDelayLocal
}

func parseEscDelay(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return time.Duration(n) * time.Millisecond, true
}

func (r *escDelayReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n, err := r.fill(p)
	if err != nil || n == 0 || r.timeout <= 0 || n == len(p) || p[n-1] != 0x1b {
		return n, err
	}
	extra, extraErr := r.readMore()
	if len(extra) == 0 {
		// Bubble Tea drops the bytes when Read returns a non-nil error.
		if extraErr != nil {
			r.pendErr = extraErr
		}
		return n, nil
	}
	copied := copy(p[n:], extra)
	if copied < len(extra) {
		r.pending = append(r.pending, extra[copied:]...)
	}
	if extraErr != nil {
		r.pendErr = extraErr
	}
	return n + copied, nil
}

func (r *escDelayReader) fill(p []byte) (int, error) {
	if len(r.pending) > 0 {
		n := copy(p, r.pending)
		r.pending = r.pending[n:]
		if len(r.pending) == 0 && r.pendErr != nil {
			err := r.pendErr
			r.pendErr = nil
			return n, err
		}
		return n, nil
	}
	if r.late != nil {
		got := <-r.late
		r.late = nil
		return r.take(p, got.b, got.err)
	}
	if r.pendErr != nil {
		err := r.pendErr
		r.pendErr = nil
		return 0, err
	}
	return r.f.Read(p)
}

func (r *escDelayReader) take(p, b []byte, err error) (int, error) {
	if len(b) == 0 {
		return 0, err
	}
	n := copy(p, b)
	if n < len(b) {
		r.pending = append(r.pending, b[n:]...)
	}
	if err != nil {
		r.pendErr = err
	}
	return n, nil
}

func (r *escDelayReader) Write(p []byte) (int, error) { return r.f.Write(p) }

func (r *escDelayReader) Close() error { return r.f.Close() }

func (r *escDelayReader) Fd() uintptr { return r.f.Fd() }

func (r *escDelayReader) Name() string { return r.f.Name() }

// compile-time checks: raw mode needs Fd, cancelreader needs Name too.
var (
	_ io.ReadWriteCloser = (*escDelayReader)(nil)
	_ interface {
		Fd() uintptr
		Name() string
	} = (*escDelayReader)(nil)
)
