package codemode

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxWorkers = 2
	maxWorkerWait     = 60 * time.Second
)

// TimeoutError is a deadline: either the queue wait or the script itself.
type TimeoutError struct {
	// Waiting is set when the script never started because no worker was free.
	Waiting bool
	Limit   time.Duration
}

func (e *TimeoutError) Error() string {
	if e.Waiting {
		return fmt.Sprintf("no codemode worker became free within %s; the script did not start", e.Limit)
	}
	return fmt.Sprintf("script timed out after %s", e.Limit)
}

type ticket struct {
	ready chan struct{}
}

var (
	poolMu     sync.Mutex
	poolActive int
	poolWait   []ticket
)

// maxWorkers is read when a slot is handed out. Only PIGO_CODEMODE_MAX_WORKERS
// is consulted. Anything that is not a positive integer is ignored.
func maxWorkers() int {
	if n, ok := positiveInt(os.Getenv("PIGO_CODEMODE_MAX_WORKERS")); ok {
		return n
	}
	return defaultMaxWorkers
}

func positiveInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// reserve waits in arrival order for a worker slot. release must be called
// after the child process has exited. On error the script did not take a slot.
func reserve(ctx context.Context, wait time.Duration) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if wait < 0 {
		wait = 0
	}
	t := ticket{ready: make(chan struct{})}
	poolMu.Lock()
	poolWait = append(poolWait, t)
	admitLocked()
	poolMu.Unlock()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-t.ready:
		return releasePool, nil
	case <-timer.C:
		if abandon(t) {
			return nil, &TimeoutError{Waiting: true, Limit: wait}
		}
		return releasePool, nil
	case <-ctx.Done():
		if abandon(t) {
			return nil, ctx.Err()
		}
		releasePool()
		return nil, ctx.Err()
	}
}

func abandon(t ticket) bool {
	poolMu.Lock()
	defer poolMu.Unlock()
	for i := range poolWait {
		if poolWait[i].ready == t.ready {
			poolWait = append(poolWait[:i], poolWait[i+1:]...)
			return true
		}
	}
	return false
}

func admitLocked() {
	limit := maxWorkers()
	for poolActive < limit && len(poolWait) > 0 {
		t := poolWait[0]
		poolWait = poolWait[1:]
		poolActive++
		close(t.ready)
	}
}

func releasePool() {
	poolMu.Lock()
	defer poolMu.Unlock()
	if poolActive > 0 {
		poolActive--
	}
	admitLocked()
}
