//go:build windows

package oauth

import "os"

func lockExclusive(*os.File) error { return nil }

func unlockFile(*os.File) error { return nil }
