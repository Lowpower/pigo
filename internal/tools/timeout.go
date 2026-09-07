package tools

import (
	"context"
	"fmt"
	"math"
	"time"
)

const maxTimeoutMS = math.MaxInt32

func timeoutContext(ctx context.Context, seconds int) (context.Context, context.CancelFunc, string) {
	if seconds <= 0 {
		return ctx, func() {}, ""
	}
	ms := seconds * 1000
	if ms > maxTimeoutMS {
		return ctx, nil, fmt.Sprintf("Invalid timeout: maximum is %g seconds", float64(maxTimeoutMS)/1000)
	}
	c, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	return c, cancel, ""
}
