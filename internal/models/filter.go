package models

import "sync"

var (
	availableIDsMu  sync.Mutex
	availableIDs    = map[string][]string{}
	availableIDsSet = map[string]bool{}
)

// SetAvailableModelIDs restricts Catalog() for provider to the given ids.
// An empty list hides every catalog model for that provider.
func SetAvailableModelIDs(provider string, ids []string) {
	availableIDsMu.Lock()
	defer availableIDsMu.Unlock()
	availableIDsSet[provider] = true
	availableIDs[provider] = append([]string(nil), ids...)
}

// ClearAvailableModelIDs removes a provider's OAuth model allowlist.
func ClearAvailableModelIDs(provider string) {
	availableIDsMu.Lock()
	defer availableIDsMu.Unlock()
	delete(availableIDs, provider)
	delete(availableIDsSet, provider)
}

func filterByAvailableIDs(provider string) func([]Model) []Model {
	return func(list []Model) []Model {
		availableIDsMu.Lock()
		ok := availableIDsSet[provider]
		ids := availableIDs[provider]
		availableIDsMu.Unlock()
		if !ok {
			return list
		}
		allow := make(map[string]bool, len(ids))
		for _, id := range ids {
			allow[id] = true
		}
		var out []Model
		for _, m := range list {
			if allow[m.ID] {
				out = append(out, m)
			}
		}
		return out
	}
}
