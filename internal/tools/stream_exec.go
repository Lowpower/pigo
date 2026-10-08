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

// formatDuration matches the shell-tool footer used by bash and powershell.
// Under a minute it keeps one decimal second. At a minute it switches to
// minutes and seconds, and at an hour it includes hours.
func formatDuration(d time.Duration) string {
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
	result := ToolBoundOutput(string(out), tempPrefix)
	emit(true)

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return appendDuration(result+fmt.Sprintf("\n[timed out after %ds]", timeoutSec), time.Since(started)), true
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return appendDuration(result+fmt.Sprintf("\n[exit code %d]", exitErr.ExitCode()), time.Since(started)), true
		}
		return appendDuration(result+"\n"+err.Error(), time.Since(started)), true
	}
	return appendDuration(result, time.Since(started)), false
}
