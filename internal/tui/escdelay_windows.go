//go:build windows

package tui

import "time"

// readMore waits up to timeout for more bytes. Deadlines do not work after
// Fd() puts the handle in blocking mode, so a timed-out Read stays in r.late
// for the next fill instead of racing a second Read on the same file.
func (r *escDelayReader) readMore() ([]byte, error) {
	ch := make(chan delayedRead, 1)
	go func() {
		buf := make([]byte, 256)
		n, err := r.f.Read(buf)
		out := append([]byte(nil), buf[:n]...)
		ch <- delayedRead{b: out, err: err}
	}()
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()
	select {
	case got := <-ch:
		return got.b, got.err
	case <-timer.C:
		r.late = ch
		return nil, nil
	}
}
