//go:build !unix

package tui

import (
	"os"
	"time"

	"github.com/Lowpower/pigo/internal/theme"
)

func readTerminalCellSize(_, _ *os.File, _ time.Duration) (int, int, bool) {
	return 0, 0, false
}

func readTerminalColors(_, _ *os.File, _ time.Duration) theme.SystemInput {
	return finishSystemInput(theme.SystemInput{})
}
