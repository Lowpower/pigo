package auth

import "sync"

var processAPIKeys sync.Map

// SetProcessAPIKey stores an in-memory API key for this process only (e.g. --api-key).
// It is not written to disk and is not copied into the environment.
func SetProcessAPIKey(provider, key string) {
	if provider == "" || key == "" {
		return
	}
	processAPIKeys.Store(provider, key)
}

// ClearProcessAPIKey drops a process-local key. Tests use this for isolation.
func ClearProcessAPIKey(provider string) {
	processAPIKeys.Delete(provider)
}

// ProcessAPIKey returns the in-memory --api-key override, or "".
func ProcessAPIKey(provider string) string {
	v, ok := processAPIKeys.Load(provider)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
