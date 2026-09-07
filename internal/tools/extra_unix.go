//go:build !windows

package tools

func extraPlatformTools(Options) []Tool { return nil }
