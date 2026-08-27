package models

import (
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
	list, err := c.List(false)
	if err != nil {
		return err
	}
	props, _ := c.Props()
	out := make([]Model, 0, len(list))
	for _, m := range list {
		if !llama.Selectable(m, props.ModelsAutoload) {
			continue
		}
		out = append(out, Model{
			Provider: llama.ProviderID,
			ID:       m.ID,
			API:      "openai-completions",
			BaseURL:  llama.InferenceURL(c.ServerURL),
		})
	}
	SetRemoteOverlay(llama.ProviderID, out)
	if store != nil {
		_ = store.Write(llama.ProviderID, StoreEntry{Models: out, CheckedAt: time.Now().UnixMilli()})
	}
	return nil
}
