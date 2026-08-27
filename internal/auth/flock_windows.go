//go:build windows

package auth

import "os"

func lockExclusive(*os.File) error { return nil }

func unlockFile(*os.File) error { return nil }
