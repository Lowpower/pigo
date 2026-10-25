//go:build darwin

package codemode

func applyHardMemoryLimit() error {
	// RLIMIT_AS does not cap resident memory on Darwin. The soft heap target
	// still aborts this process when an allocation cannot be reclaimed.
	return nil
}
