//go:build windows

package ai

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func osRelease() string {
	v := windows.RtlGetVersion()
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
