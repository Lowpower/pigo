//go:build !unix

package tui

func drainPendingTTY() {}
