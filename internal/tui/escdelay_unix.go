//go:build unix

package tui

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

// readMore waits up to timeout for more bytes. A timeout is (nil, nil):
// the caller already has a lone ESC to deliver.
func (r *escDelayReader) readMore() ([]byte, error) {
	deadline := time.Now().Add(r.timeout)
	fd := int32(r.f.Fd())
	for {
		wait := time.Until(deadline)
		if wait <= 0 {
			return nil, nil
		}
		ms := int(wait.Milliseconds())
		if ms < 1 {
			ms = 1
		}
		fds := []unix.PollFd{{Fd: fd, Events: unix.POLLIN}}
		n, err := unix.Poll(fds, ms)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n == 0 {
			return nil, err
		}
		buf := make([]byte, 256)
		rn, rerr := r.f.Read(buf)
		if rn == 0 {
			return nil, rerr
		}
		out := make([]byte, rn)
		copy(out, buf[:rn])
		return out, rerr
	}
}
