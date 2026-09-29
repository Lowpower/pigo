//go:build !unix

package tui

import (
	"os"
	"time"

	"github.com/Lowpower/pigo/internal/theme"
)

func readTerminalColors(_, _ *os.File, _ time.Duration) theme.SystemInput {
	return finishSystemInput(theme.SystemInput{})
}
