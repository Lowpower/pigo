package models

import (
	"sort"
	"strings"
	"sync"
)

// ProviderSpec is a registerable provider: catalog, default API, and optional hooks.
type ProviderSpec struct {
	ID         string
	Env        []string
	BaseURL    string
	DefaultAPI string
	DefaultID  string
	Models     []Model
	// FilterModels optionally restricts catalog (e.g. GitHub Copilot).
	FilterModels func([]Model) []Model
	// RefreshModels optionally pulls a remote/dynamic catalog into store.
	RefreshModels func(store CatalogStore) error
}

var (
	regMu     sync.Mutex
	providers = map[string]ProviderSpec{}
	regOrder  []string
)

// RegisterProvider adds or replaces a provider in the catalog registry.
func RegisterProvider(p ProviderSpec) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, ok := providers[p.ID]; !ok {
		regOrder = append(regOrder, p.ID)
	}
	providers[p.ID] = p
}

// LookupProvider returns a registered provider spec.
func LookupProvider(id string) (ProviderSpec, bool) {
	regMu.Lock()
	defer regMu.Unlock()
	p, ok := providers[id]
	return p, ok
}

// ProviderIDs returns registered provider ids in registration order.
func ProviderIDs() []string {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]string, len(regOrder))
	copy(out, regOrder)
	return out
}

// Lookup finds a catalog model by provider and id.
func Lookup(provider, id string) (Model, bool) {
	for _, m := range Catalog() {
		if m.Provider == provider && m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// CacheReadPerToken is catalog cache-read dollars per token, or 0.
func CacheReadPerToken(provider, id string) float64 {
	m, ok := Lookup(provider, id)
	if !ok || m.Cost == nil {
		return 0
	}
	return m.Cost.CacheRead / 1_000_000
}

// DefaultAPI is the API used when a model id is not in the catalog.
func DefaultAPI(provider string) string {
	if p, ok := LookupProvider(provider); ok && p.DefaultAPI != "" {
		return p.DefaultAPI
	}
	return ""
}

// DefaultID is the preferred model id for a provider.
func DefaultID(provider string) string {
	if p, ok := LookupProvider(provider); ok && p.DefaultID != "" {
		return p.DefaultID
	}
	return ""
}

// APIFor returns the catalog API for provider/model, else the provider default.
func APIFor(provider, id string) string {
	if m, ok := Lookup(provider, id); ok && m.API != "" {
		return m.API
	}
	return DefaultAPI(provider)
}

// Catalog is the built-in (plus overlay) model list.
func Catalog() []Model {
	regMu.Lock()
	specs := make([]ProviderSpec, 0, len(regOrder))
	for _, id := range regOrder {
		specs = append(specs, providers[id])
	}
	regMu.Unlock()

	var out []Model
	seen := map[string]bool{}
	for _, p := range specs {
		list := append([]Model(nil), p.Models...)
		list = applyOverlays(p.ID, list)
		if p.FilterModels != nil {
			list = p.FilterModels(list)
		}
		for _, m := range list {
			if m.Provider == "" {
				m.Provider = p.ID
			}
			if m.API == "" {
				m.API = p.DefaultAPI
			}
			key := m.Provider + "/" + m.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, m)
		}
	}
	return out
}

// Available returns catalog entries whose provider is in authenticated.
func Available(authenticated []string) []Model {
	allow := map[string]bool{}
	for _, id := range authenticated {
		allow[id] = true
	}
	var out []Model
	for _, m := range Catalog() {
		if allow[m.Provider] {
			out = append(out, m)
		}
	}
	return out
}

// Search filters the catalog by a substring of provider/id/api.
func Search(q string) []Model {
	return SearchIn(Catalog(), q)
}

// SearchIn filters models by a substring of provider/id/api.
func SearchIn(list []Model, q string) []Model {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return list
	}
	var out []Model
	for _, m := range list {
		hay := strings.ToLower(m.Provider + "/" + m.ID + " " + m.API)
		if strings.Contains(hay, q) {
			out = append(out, m)
		}
	}
	return out
}

// PickOpts selects the initial provider/model.
type PickOpts struct {
	CLIProvider   string
	CLIModel      string
	SavedProvider string
	SavedModel    string
	Authenticated []string
}

var pickOrder = []string{"opencode", "anthropic", "openai", "google", "amazon-bedrock"}

// PickInitial chooses a model: CLI, then authenticated saved default, then
// the first authenticated provider in pickOrder.
func PickInitial(o PickOpts) Model {
	auth := map[string]bool{}
	for _, id := range o.Authenticated {
		auth[id] = true
	}
	if o.CLIProvider != "" {
		return resolveOrSynthetic(o.CLIProvider, o.CLIModel)
	}
	if o.SavedProvider != "" && auth[o.SavedProvider] {
		return resolveOrSynthetic(o.SavedProvider, o.SavedModel)
	}
	for _, id := range pickOrder {
		if auth[id] {
			return resolveOrSynthetic(id, DefaultID(id))
		}
	}
	ids := append([]string(nil), o.Authenticated...)
	sort.Strings(ids)
	if len(ids) > 0 {
		return resolveOrSynthetic(ids[0], DefaultID(ids[0]))
	}
	return Model{}
}

func resolveOrSynthetic(provider, id string) Model {
	if id == "" {
		id = DefaultID(provider)
	}
	if m, ok := Lookup(provider, id); ok {
		return m
	}
	return Model{Provider: provider, ID: id, API: DefaultAPI(provider)}
}

func applyOverlays(providerID string, list []Model) []Model {
	list = mergeOverlay(list, remoteOverlay(providerID))
	list = mergeOverlay(list, userOverlay(providerID))
	return list
}

func mergeOverlay(base, extra []Model) []Model {
	if len(extra) == 0 {
		return base
	}
	out := append([]Model(nil), base...)
	idx := map[string]int{}
	for i, m := range out {
		idx[m.ID] = i
	}
	for _, m := range extra {
		if i, ok := idx[m.ID]; ok {
			if m.API != "" {
				out[i].API = m.API
			}
			if m.BaseURL != "" {
				out[i].BaseURL = m.BaseURL
			}
			continue
		}
		idx[m.ID] = len(out)
		out = append(out, m)
	}
	return out
}
