//go:build !unix && !windows

package ai

func osRelease() string { return "" }
