package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/llama"
	"github.com/Lowpower/pigo/internal/models"
)

func argFloat(args map[string]any, key string) float64 {
	if args == nil {
		return 0
	}
	switch n := args[key].(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

func filterProvider(list []models.Model, provider string) []models.Model {
	if provider == "" {
		return list
	}
	var out []models.Model
	for _, m := range list {
		if m.Provider == provider {
			out = append(out, m)
		}
	}
	return out
}

// availableModels is the authenticated catalog. kind "" is every type.
// OpenAI Decisions models stay hidden while the stored credential is ChatGPT OAuth.
// A process API key (--api-key) wins, matching auth.Resolve.
func (e *Engine) availableModels(kind string) []models.Model {
	ids := auth.AuthenticatedIDs(auth.Open(e.Opts.AgentDir))
	var list []models.Model
	if kind == "" {
		list = models.AllAvailable(ids)
	} else {
		list = models.AvailableOfType(kind, ids)
	}
	if !e.openAIUsesOAuth() {
		return list
	}
	var out []models.Model
	for _, m := range list {
		if m.Provider == "openai" && m.ModelType() == "classifier" {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (e *Engine) openAIUsesOAuth() bool {
	if auth.ProcessAPIKey("openai") != "" {
		return false
	}
	cred, ok := auth.Get(e.Opts.AgentDir, "openai")
	return ok && cred.Type == auth.TypeOAuth
}

func (e *Engine) doHostClassify(args map[string]any) map[string]any {
	provider, id := classifyTarget(args)
	if e.disableLlama && provider == llama.ProviderID {
		return classifyMap(ai.ClassifyResult{
			Provider: provider, Model: id, Answers: map[string]any{},
			StopReason: "error", ErrorMessage: fmt.Sprintf("no classifier %s/%s", provider, id),
		})
	}
	model, ok := models.ModelOfType("classifier", provider, id)
	if !ok {
		return classifyMap(ai.ClassifyResult{
			Provider: provider, Model: id, Answers: map[string]any{},
			StopReason: "error", ErrorMessage: fmt.Sprintf("no classifier %s/%s", provider, id),
		})
	}
	cfg, source, err := classifyClient(e.Opts.AgentDir, provider)
	if err != nil {
		return classifyMap(ai.ClassifyResult{
			API: model.API, Provider: model.Provider, Model: model.ID, Answers: map[string]any{},
			StopReason: "error", ErrorMessage: err.Error(),
		})
	}
	if model.API == "openai-decisions" && source == "OAuth" {
		return classifyMap(ai.ClassifyResult{
			API: model.API, Provider: model.Provider, Model: model.ID, Answers: map[string]any{},
			StopReason: "error", ErrorMessage: "openai decisions requires an API key",
		})
	}
	if spec, ok := models.LookupProvider(provider); ok && len(spec.Headers) > 0 {
		merged := make(map[string]string, len(spec.Headers)+len(cfg.Headers))
		for k, v := range spec.Headers {
			merged[k] = v
		}
		for k, v := range cfg.Headers {
			merged[k] = v
		}
		cfg.Headers = merged
	}
	var images []ai.ClassifyImage
	if raw, ok := args["images"].([]any); ok {
		for _, item := range raw {
			img, ok := item.(map[string]any)
			if !ok {
				continue
			}
			images = append(images, ai.ClassifyImage{
				Data: argString(img, "data"),
				MIME: argString(img, "mimeType"),
			})
		}
	}
	res := ai.Classify(context.Background(), model, ai.ClassifyContext{
		State:       asStringMap(args["state"]),
		Questions:   asStringMap(args["questions"]),
		Images:      images,
		Temperature: argFloat(args, "temperature"),
	}, cfg)
	if e.Opts.Session != nil && res.Usage != nil {
		_, _ = e.Opts.Session.AppendUsage("classify", res.Provider, res.Model, *res.Usage, "")
	}
	return classifyMap(res)
}

func classifyTarget(args map[string]any) (provider, id string) {
	provider = argString(args, "provider")
	id = argString(args, "id")
	if model := asStringMap(args["model"]); model != nil {
		if provider == "" {
			provider = argString(model, "provider")
		}
		if id == "" {
			id = argString(model, "id")
		}
	}
	return provider, id
}

func classifyClient(agentDir, provider string) (ai.ClientConfig, string, error) {
	p, ok := auth.Lookup(provider)
	if !ok {
		return ai.ClientConfig{}, "", fmt.Errorf("unknown provider %s", provider)
	}
	res, err := auth.Resolve(context.Background(), auth.Open(agentDir), p, auth.ResolveOpts{})
	if err != nil {
		return ai.ClientConfig{}, "", err
	}
	if res == nil {
		key := auth.APIKey(agentDir, provider)
		if key == "" {
			return ai.ClientConfig{}, "", fmt.Errorf("no API key for %s", provider)
		}
		return ai.ClientConfig{APIKey: key}, "api_key", nil
	}
	key := res.Auth.APIKey
	if key == "" {
		key = auth.Secret(res)
	}
	if key == "" && len(res.Auth.Headers) == 0 {
		return ai.ClientConfig{}, res.Source, fmt.Errorf("no API key for %s", provider)
	}
	return ai.ClientConfig{APIKey: key, BaseURL: res.Auth.BaseURL, Headers: res.Auth.Headers}, res.Source, nil
}

func classifyMap(res ai.ClassifyResult) map[string]any {
	b, err := json.Marshal(res)
	if err != nil {
		return map[string]any{"stopReason": "error", "errorMessage": err.Error(), "answers": map[string]any{}}
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil || out == nil {
		return map[string]any{"stopReason": "error", "errorMessage": "could not encode classifier result", "answers": map[string]any{}}
	}
	return out
}
