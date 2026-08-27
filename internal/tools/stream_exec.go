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

func runStreamed(ctx context.Context, cmd *exec.Cmd, timeoutSec int, tempPrefix string) (string, bool) {
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
		return result + fmt.Sprintf("\n[timed out after %ds]", timeoutSec), true
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return result + fmt.Sprintf("\n[exit code %d]", exitErr.ExitCode()), true
		}
		return result + "\n" + err.Error(), true
	}
	return result, false
}
