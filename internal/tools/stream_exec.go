package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/shell"
)

const shellUpdateThrottle = 100 * time.Millisecond

// FormatDuration is the shell-tool duration text shared by Took and Elapsed.
// Under a minute it keeps one decimal second. At a minute it switches to
// minutes and seconds, and at an hour it includes hours.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := float64(d) / float64(time.Second)
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	total := int(seconds)
	minutes := total / 60
	remainder := total % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, remainder)
	}
	return fmt.Sprintf("%dh %dm %ds", minutes/60, minutes%60, remainder)
}

func formatDuration(d time.Duration) string { return FormatDuration(d) }

func appendDuration(result string, d time.Duration) string {
	return result + "\nTook " + formatDuration(d)
}

func runStreamed(ctx context.Context, cmd *exec.Cmd, timeoutSec int, tempPrefix string) (string, bool) {
	started := time.Now()
	onUpdate := OutputUpdate(ctx)
	var mu sync.Mutex
	acc := ""
	lastEmit := time.Time{}
	emit := func(force bool) {
		if onUpdate == nil {
			return
		}
		mu.Lock()
		snap := acc
		if !force && !lastEmit.IsZero() && time.Since(lastEmit) < shellUpdateThrottle {
			mu.Unlock()
			return
		}
		lastEmit = time.Now()
		mu.Unlock()
		onUpdate(TruncateTail(snap, DefaultMaxLines, DefaultMaxBytes).Content)
	}

	out, err := shell.WaitStream(cmd, func(chunk string) {
		mu.Lock()
		acc += chunk
		mu.Unlock()
		emit(false)
	})
	full := string(out)
	result, path := modelShellText(full, tempPrefix)
	emit(true)
	elapsed := time.Since(started)

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return appendDuration(result+fmt.Sprintf("\n[timed out after %ds]", timeoutSec), elapsed), true
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if ctx.Err() == nil {
				storeShellStructured(ctx, full, path, tempPrefix, exitErr.ExitCode(), elapsed)
			}
			return appendDuration(result+fmt.Sprintf("\n[exit code %d]", exitErr.ExitCode()), elapsed), true
		}
		return appendDuration(result+"\n"+err.Error(), elapsed), true
	}
	if ctx.Err() == nil {
		storeShellStructured(ctx, full, path, tempPrefix, 0, elapsed)
	}
	return appendDuration(result, elapsed), false
}
