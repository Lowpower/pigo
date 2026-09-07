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
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	if _, err := os.Stat(abs); err == nil {
		if dir, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
			return filepath.Join(dir, filepath.Base(abs))
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
