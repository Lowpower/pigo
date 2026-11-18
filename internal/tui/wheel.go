package tui

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Lines moved per mouse-wheel event. "auto" accelerates fast wheel spins.
// The curve matches pi v1.3.0 packages/tui/src/wheel-scroll.ts.

const (
	// Several events closer than this belong to one physical notch (Ghostty
	// emits them ~4 ms apart) or come from a high-resolution source.
	burstGapMS = 5
	// A pause longer than this ends a scroll gesture.
	gestureGapMS = 200
	// Average event gap that maps to one line per event.
	referenceGapMS = 100
	maxAutoLines   = 6
)

// wheelScrollAccelerator converts wheel events into line counts.
type wheelScrollAccelerator struct {
	lines         float64
	auto          bool
	accelerate    bool
	hasLast       bool
	lastTime      int64
	lastDirection int
	hasAvg        bool
	averageGap    float64
	carry         float64
}

func newWheelScrollAccelerator(lines any, accelerate bool) *wheelScrollAccelerator {
	a := &wheelScrollAccelerator{accelerate: accelerate}
	a.setLines(lines)
	return a
}

func (a *wheelScrollAccelerator) setLines(lines any) {
	a.auto, a.lines = parseWheelLines(lines)
	a.reset()
}

// next returns the positive line count for a wheel event in direction at now
// (milliseconds). direction is -1 or 1.
func (a *wheelScrollAccelerator) next(direction int, now int64) int {
	if !a.auto {
		if math.IsNaN(a.lines) || math.IsInf(a.lines, 0) {
			return 1
		}
		n := int(math.Floor(a.lines))
		if n < 1 {
			return 1
		}
		return n
	}
	if !a.accelerate {
		return 1
	}

	gap := 0.0
	same := false
	if a.hasLast {
		gap = float64(now - a.lastTime)
		same = direction == a.lastDirection && gap <= gestureGapMS
	}
	a.lastTime = now
	a.lastDirection = direction
	a.hasLast = true
	if !same {
		a.hasAvg = false
		a.carry = 0
		return 1
	}
	if gap < burstGapMS {
		return 1
	}
	if !a.hasAvg {
		a.averageGap = gap
		a.hasAvg = true
	} else {
		a.averageGap = (a.averageGap + gap) / 2
	}
	lines := math.Min(maxAutoLines, math.Max(1, referenceGapMS/a.averageGap)) + a.carry
	whole := math.Floor(lines)
	a.carry = lines - whole
	return int(whole)
}

func (a *wheelScrollAccelerator) reset() {
	a.hasLast = false
	a.lastTime = 0
	a.lastDirection = 0
	a.hasAvg = false
	a.averageGap = 0
	a.carry = 0
}

func parseWheelLines(v any) (auto bool, lines float64) {
	switch x := v.(type) {
	case string:
		s := strings.TrimSpace(x)
		if s == "" || strings.EqualFold(s, "auto") {
			return true, 0
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return false, math.NaN()
		}
		return false, f
	case float64:
		return false, x
	case float32:
		return false, float64(x)
	case int:
		return false, float64(x)
	case int64:
		return false, float64(x)
	case int32:
		return false, float64(x)
	default:
		return true, 0
	}
}

// terminalAcceleratesWheel reports whether the local terminal already turns
// one wheel notch into one line. That is a macOS session that is not SSH.
func terminalAcceleratesWheel() bool {
	return wheelAlreadyAccelerated(runtime.GOOS, sshEnv("SSH_CONNECTION"), sshEnv("SSH_CLIENT"), sshEnv("SSH_TTY"))
}

func sshEnv(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok {
		return ""
	}
	if v == "" {
		return "set"
	}
	return v
}

func wheelAlreadyAccelerated(goos, sshConnection, sshClient, sshTTY string) bool {
	if goos != "darwin" {
		return false
	}
	return sshConnection == "" && sshClient == "" && sshTTY == ""
}
