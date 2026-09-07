package tools

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	mutationMu    sync.Mutex
	mutationLocks = map[string]*sync.Mutex{}
)

// withFileMutation serializes edit/write operations that target the same file.
func withFileMutation(path string, fn func() (string, bool)) (string, bool) {
	key := mutationKey(path)
	mu := lockFor(key)
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

func mutationKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	if _, err := os.Stat(abs); err == nil {
		if real, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
			return filepath.Join(real, filepath.Base(abs))
		}
	}
	return abs
}

func lockFor(key string) *sync.Mutex {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	mu, ok := mutationLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		mutationLocks[key] = mu
	}
	return mu
}
