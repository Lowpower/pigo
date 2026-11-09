package models

import (
	"math"
	"os"
	"time"

	"github.com/Lowpower/pigo/internal/llama"
)

func refreshLlama(store CatalogStore) error {
	url := os.Getenv("LLAMA_BASE_URL")
	if url == "" {
		return nil
	}
	c, err := llama.NewClient(url, os.Getenv("LLAMA_API_KEY"))
	if err != nil {
		return err
	}
	return ApplyLlamaCatalog(c, store)
}

// ApplyLlamaCatalog replaces the llama.cpp overlay from a live router client.
func ApplyLlamaCatalog(c *llama.Client, store CatalogStore) error {
	if c == nil {
		return nil
	}
	list, err := c.List(true)
	if err != nil {
		return err
	}
	props, _ := c.Props()
	out := make([]Model, 0, len(list))
	for _, m := range list {
		if !llama.Selectable(m, props.ModelsAutoload) {
			continue
		}
		cw := 0
		if n := llama.ContextSize(m); n > 0 && n <= int64(math.MaxInt) {
			cw = int(n)
		}
		base := llama.InferenceURL(c.ServerURL)
		clf := Model{
			Provider:      llama.ProviderID,
			ID:            m.ID,
			Type:          "classifier",
			API:           "llama-cpp-classify",
			BaseURL:       c.ServerURL,
			Input:         []string{"text"},
			ContextWindow: cw,
		}
		if llama.HasOutputModality(m, "decisions") {
			clf.API = "typesafe-system-one"
			clf.BaseURL = base
		}
		if llama.DecisionOnly(m) {
			out = append(out, clf)
			continue
		}
		out = append(out, Model{
			Provider:      llama.ProviderID,
			ID:            m.ID,
			API:           "openai-completions",
			BaseURL:       base,
			ContextWindow: cw,
		}, clf)
	}
	SetRemoteOverlay(llama.ProviderID, out)
	if store != nil {
		_ = store.Write(llama.ProviderID, StoreEntry{Models: out, CheckedAt: time.Now().UnixMilli()})
	}
	return nil
}
