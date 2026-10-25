//go:build !linux && !darwin && !windows

package codemode

func applyHardMemoryLimit() error { return nil }
