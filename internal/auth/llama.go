package auth

import (
	"fmt"
	"os"
	"strings"

	"github.com/Lowpower/pigo/internal/llama"
)

func llamaLogin(ix Interaction) (Credential, error) {
	url := llama.DefaultServerURL
	if v := strings.TrimSpace(os.Getenv("LLAMA_BASE_URL")); v != "" {
		url = v
	}
	if ix.Prompt == nil {
		return Credential{}, fmt.Errorf("no prompt available")
	}
	entered, err := ix.Prompt(Prompt{Type: PromptText, Message: "llama.cpp server URL:", Placeholder: url})
	if err != nil {
		return Credential{}, err
	}
	if strings.TrimSpace(entered) != "" {
		url = entered
	}
	key, err := ix.Prompt(Prompt{Type: PromptSecret, Message: "API key (optional):"})
	if err != nil {
		return Credential{}, err
	}
	norm, err := llama.NormalizeServerURL(url)
	if err != nil {
		return Credential{}, err
	}
	c, err := llama.NewClient(norm, strings.TrimSpace(key))
	if err != nil {
		return Credential{}, err
	}
	if _, err := c.List(false); err != nil {
		return Credential{}, err
	}
	return Credential{Type: TypeAPIKey, Key: strings.TrimSpace(key), Env: map[string]string{"LLAMA_BASE_URL": norm}}, nil
}
