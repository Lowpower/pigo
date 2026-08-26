package auth

import (
	"context"
	"errors"
	"time"
)

const (
	deviceCancelMessage       = "login cancelled"
	deviceTimeoutMessage      = "device flow timed out"
	deviceSlowDownTimeoutMsg  = "device flow timed out after one or more slow_down responses"
	deviceMinIntervalMs       = 1000
	deviceDefaultIntervalSec  = 5
	deviceSlowDownIncrementMs = 5000
)

type devicePollStatus int

const (
	devicePending devicePollStatus = iota
	deviceSlowDown
	deviceFailed
	deviceComplete
)

type devicePollResult[T any] struct {
	status          devicePollStatus
	value           T
	message         string
	intervalSeconds int
}

func abortableSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return errors.New(deviceCancelMessage)
	case <-t.C:
		return nil
	}
}

func pollDeviceCode[T any](ctx context.Context, intervalSec, expiresSec int, waitFirst bool, poll func() (devicePollResult[T], error)) (T, error) {
	var zero T
	deadline := time.Now().Add(365 * 24 * time.Hour)
	if expiresSec > 0 {
		deadline = time.Now().Add(time.Duration(expiresSec) * time.Second)
	}
	intervalMs := deviceDefaultIntervalSec * 1000
	if intervalSec > 0 {
		intervalMs = intervalSec * 1000
	}
	if intervalMs < deviceMinIntervalMs {
		intervalMs = deviceMinIntervalMs
	}
	slowDowns := 0
	if waitFirst {
		rem := time.Until(deadline)
		if rem > 0 {
			wait := time.Duration(intervalMs) * time.Millisecond
			if wait > rem {
				wait = rem
			}
			if err := abortableSleep(ctx, wait); err != nil {
				return zero, err
			}
		}
	}
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return zero, errors.New(deviceCancelMessage)
		}
		res, err := poll()
		if err != nil {
			return zero, err
		}
		switch res.status {
		case deviceComplete:
			return res.value, nil
		case deviceFailed:
			return zero, errors.New(res.message)
		case deviceSlowDown:
			slowDowns++
			if res.intervalSeconds > 0 {
				intervalMs = res.intervalSeconds * 1000
				if intervalMs < deviceMinIntervalMs {
					intervalMs = deviceMinIntervalMs
				}
			} else {
				intervalMs += deviceSlowDownIncrementMs
				if intervalMs < deviceMinIntervalMs {
					intervalMs = deviceMinIntervalMs
				}
			}
		}
		rem := time.Until(deadline)
		if rem <= 0 {
			break
		}
		wait := time.Duration(intervalMs) * time.Millisecond
		if wait > rem {
			wait = rem
		}
		if err := abortableSleep(ctx, wait); err != nil {
			return zero, err
		}
	}
	if slowDowns > 0 {
		return zero, errors.New(deviceSlowDownTimeoutMsg)
	}
	return zero, errors.New(deviceTimeoutMessage)
}
