package ai

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/Lowpower/pigo/internal/models"
)

const openaiPromptCacheKeyMax = 64

func reasoningEffort(opts Options) string {
	_, effort := resolvedThinking(opts)
	return effort
}

// resolvedThinking clamps the requested level onto the current model and
// returns the provider effort string. off stays off when the map marks it
// unsupported. An unknown model keeps the historical raw level.
func resolvedThinking(opts Options) (canonical, effort string) {
	level := strings.ToLower(strings.TrimSpace(opts.Thinking))
	m, ok := models.Lookup(opts.Provider, opts.Model)
	if !ok {
		if level == "" || level == "off" {
			return level, ""
		}
		return level, level
	}
	canonical = m.RequestThinkingLevel(level)
	if canonical == "" {
		return "", ""
	}
	wire, send := m.ProviderThinkingValue(canonical)
	if !send {
		return canonical, ""
	}
	return canonical, wire
}

func thinkingOn(canonical string) bool {
	return canonical != "" && canonical != "off"
}

func offExplicitlyUnsupported(opts Options) bool {
	m, ok := models.Lookup(opts.Provider, opts.Model)
	if !ok {
		return false
	}
	mapped, present := m.ThinkingLevelMap["off"]
	if !present {
		return false
	}
	return mapped == nil || strings.TrimSpace(*mapped) == ""
}

func clampPromptCacheKey(key string) string {
	if key == "" {
		return ""
	}
	runes := []rune(key)
	if len(runes) <= openaiPromptCacheKeyMax {
		return key
	}
	return string(runes[:openaiPromptCacheKeyMax])
}

func resolveWireAPI(provider, model, api string) string {
	switch provider {
	case "github-copilot":
		return copilotStreamAPI(provider, model, api)
	case "opencode":
		if api == "" || api == "opencode" {
			return guessOpenCodeAPI(model)
		}
		return api
	case "opencode-go":
		return overlayOrGuess(provider, model, api, guessOpenCodeGoAPI)
	case "cloudflare-ai-gateway":
		return overlayOrGuess(provider, model, api, guessCloudflareGatewayAPI)
	case "fireworks":
		return overlayOrGuess(provider, model, api, guessFireworksAPI)
	default:
		return api
	}
}

func overlayOrGuess(provider, model, api string, guess func(string) string) string {
	spec, ok := models.LookupProvider(provider)
	if !ok {
		return api
	}
	if m, found := models.Lookup(provider, model); found && m.API != "" && m.API != spec.DefaultAPI {
		return m.API
	}
	if guessed := guess(model); guessed != "" {
		return guessed
	}
	return api
}

func guessOpenCodeAPI(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(id, "claude"):
		return "anthropic-messages"
	case strings.HasPrefix(id, "gemini"):
		return "google-generative-ai"
	case strings.HasPrefix(id, "gpt-5"),
		strings.HasPrefix(id, "gpt-4o"),
		strings.HasPrefix(id, "o1"),
		strings.HasPrefix(id, "o3"),
		strings.HasPrefix(id, "o4"),
		strings.Contains(id, "codex"):
		return "openai-responses"
	default:
		return "openai-completions"
	}
}

func guessOpenCodeGoAPI(model string) string {
	if guessOpenCodeAPI(model) == "google-generative-ai" {
		return ""
	}
	return guessOpenCodeAPI(model)
}

func guessCloudflareGatewayAPI(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(id, "claude") || strings.Contains(id, "anthropic"):
		return "anthropic-messages"
	case strings.HasPrefix(id, "gpt-5") || strings.HasPrefix(id, "o1") || strings.HasPrefix(id, "o3") || strings.Contains(id, "openai/"):
		return "openai-responses"
	case strings.Contains(id, "gpt") || strings.Contains(id, "llama") || strings.Contains(id, "compat"):
		return "openai-completions"
	default:
		return ""
	}
}

func cloudflareGatewaySuffix(api string) string {
	switch api {
	case "anthropic-messages":
		return "/anthropic"
	case "openai-responses":
		return "/openai"
	case "openai-completions":
		return "/compat"
	default:
		return ""
	}
}

func cloudflareGatewayBaseURL(api, base string) string {
	want := cloudflareGatewaySuffix(api)
	if want == "" || base == "" {
		return base
	}
	trimmed := strings.TrimRight(base, "/")
	for _, suf := range []string{"/anthropic", "/openai", "/compat"} {
		if strings.HasSuffix(trimmed, suf) {
			return strings.TrimSuffix(trimmed, suf) + want
		}
	}
	return base
}

func guessFireworksAPI(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(id, "claude") || strings.Contains(id, "anthropic"):
		return "anthropic-messages"
	case strings.Contains(id, "glm") || strings.Contains(id, "kimi") || strings.Contains(id, "qwen") ||
		strings.Contains(id, "gpt") || strings.Contains(id, "llama"):
		return "openai-completions"
	default:
		return ""
	}
}

func azureDeploymentName(model string) string {
	raw := strings.TrimSpace(os.Getenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP"))
	if raw == "" || model == "" {
		return model
	}
	// JSON object {"gpt-4":"my-dep"} or simple "src=dst,src2=dst2"
	if strings.HasPrefix(raw, "{") {
		var m map[string]string
		if json.Unmarshal([]byte(raw), &m) == nil {
			if dep, ok := m[model]; ok && dep != "" {
				return dep
			}
		}
		return model
	}
	for _, part := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(k) == model && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return model
}

func azureAPIVersion() string {
	if v := strings.TrimSpace(os.Getenv("AZURE_OPENAI_API_VERSION")); v != "" {
		return v
	}
	return "v1"
}

func googleThinkingLevel(effort string) string {
	switch strings.ToLower(effort) {
	case "minimal":
		return "MINIMAL"
	case "low":
		return "LOW"
	case "medium":
		return "MEDIUM"
	case "high", "xhigh", "max":
		return "HIGH"
	default:
		return ""
	}
}

// googleMappedLevel keeps the four Google enums in SDK spelling and passes
// any other mapped provider value through unchanged.
func googleMappedLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "minimal", "low", "medium", "high":
		return googleThinkingLevel(value)
	default:
		return strings.TrimSpace(value)
	}
}

func googleThinkingWire(opts Options) string {
	canonical, _ := resolvedThinking(opts)
	level := googleThinkingLevel(canonical)
	m, ok := models.Lookup(opts.Provider, opts.Model)
	if !ok {
		return level
	}
	mapped, present := m.ThinkingLevelMap[canonical]
	if !present || mapped == nil {
		return level
	}
	if value := strings.TrimSpace(*mapped); value != "" {
		return googleMappedLevel(value)
	}
	return level
}
