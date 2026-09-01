package models

type extraProvider struct {
	ID         string
	Name       string
	DefaultAPI string
	DefaultID  string
	BaseURL    string
	Env        []string
	Headers    map[string]string
}

// Offline fallback ids come from the current pi.dev catalog (one model each).
// PrepareCatalog overlays the remote list when network is allowed.
var extraProviders = []extraProvider{
	{ID: "ant-ling", Name: "Ant Ling API key", DefaultAPI: "openai-completions", DefaultID: "Ling-2.6-1T", BaseURL: "https://api.ant-ling.com/v1", Env: []string{"ANT_LING_API_KEY"}},
	{ID: "baseten", Name: "Baseten API key", DefaultAPI: "openai-completions", DefaultID: "deepseek-ai/DeepSeek-V4-Flash-0731", BaseURL: "https://inference.baseten.co/v1", Env: []string{"BASETEN_API_KEY"}},
	{ID: "cerebras", Name: "Cerebras API key", DefaultAPI: "openai-completions", DefaultID: "gemma-4-31b", BaseURL: "https://api.cerebras.ai/v1", Env: []string{"CEREBRAS_API_KEY"}},
	{ID: "deepseek", Name: "DeepSeek API key", DefaultAPI: "openai-completions", DefaultID: "deepseek-v4-flash", BaseURL: "https://api.deepseek.com", Env: []string{"DEEPSEEK_API_KEY"}},
	{ID: "groq", Name: "Groq API key", DefaultAPI: "openai-completions", DefaultID: "llama-3.1-8b-instant", BaseURL: "https://api.groq.com/openai/v1", Env: []string{"GROQ_API_KEY"}},
	{ID: "huggingface", Name: "Hugging Face token", DefaultAPI: "openai-completions", DefaultID: "MiniMaxAI/MiniMax-M2", BaseURL: "https://router.huggingface.co/v1", Env: []string{"HF_TOKEN"}},
	{ID: "kimi-coding", Name: "Kimi API key", DefaultAPI: "anthropic-messages", DefaultID: "k3", BaseURL: "https://api.kimi.com/coding", Env: []string{"KIMI_API_KEY"}},
	{ID: "minimax", Name: "MiniMax API key", DefaultAPI: "anthropic-messages", DefaultID: "MiniMax-M2.7", BaseURL: "https://api.minimax.io/anthropic", Env: []string{"MINIMAX_API_KEY"}},
	{ID: "minimax-cn", Name: "MiniMax CN API key", DefaultAPI: "anthropic-messages", DefaultID: "MiniMax-M2.7", BaseURL: "https://api.minimaxi.com/anthropic", Env: []string{"MINIMAX_CN_API_KEY"}},
	{ID: "moonshotai", Name: "Moonshot AI API key", DefaultAPI: "openai-completions", DefaultID: "kimi-k2-0711-preview", BaseURL: "https://api.moonshot.ai/v1", Env: []string{"MOONSHOT_API_KEY"}},
	{ID: "moonshotai-cn", Name: "Moonshot AI API key", DefaultAPI: "openai-completions", DefaultID: "kimi-k2-0711-preview", BaseURL: "https://api.moonshot.cn/v1", Env: []string{"MOONSHOT_API_KEY"}},
	{ID: "nvidia", Name: "NVIDIA API key", DefaultAPI: "openai-completions", DefaultID: "deepseek-ai/deepseek-v4-flash-0731", BaseURL: "https://integrate.api.nvidia.com/v1", Env: []string{"NVIDIA_API_KEY"}, Headers: map[string]string{"NVCF-POLL-SECONDS": "3600"}},
	{ID: "openrouter", Name: "OpenRouter API key", DefaultAPI: "openai-completions", DefaultID: "aion-labs/aion-2.0", BaseURL: "https://openrouter.ai/api/v1", Env: []string{"OPENROUTER_API_KEY"}},
	{ID: "qwen-token-plan", Name: "Qwen Token Plan API key", DefaultAPI: "openai-completions", DefaultID: "MiniMax-M2.5", BaseURL: "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1", Env: []string{"QWEN_TOKEN_PLAN_API_KEY"}},
	{ID: "qwen-token-plan-cn", Name: "Qwen Token Plan CN API key", DefaultAPI: "openai-completions", DefaultID: "MiniMax-M2.5", BaseURL: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", Env: []string{"QWEN_TOKEN_PLAN_CN_API_KEY"}},
	{ID: "qwen-token-plan-individual", Name: "Qwen Token Plan Individual API key", DefaultAPI: "openai-completions", DefaultID: "deepseek-v4-flash-0731", BaseURL: "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1", Env: []string{"QWEN_TOKEN_PLAN_API_KEY"}},
	{ID: "together", Name: "Together API key", DefaultAPI: "openai-completions", DefaultID: "MiniMaxAI/MiniMax-M2.7", BaseURL: "https://api.together.ai/v1", Env: []string{"TOGETHER_API_KEY"}},
	{ID: "vercel-ai-gateway", Name: "Vercel AI Gateway API key", DefaultAPI: "anthropic-messages", DefaultID: "alibaba/qwen-3-14b", BaseURL: "https://ai-gateway.vercel.sh", Env: []string{"AI_GATEWAY_API_KEY"}},
	{ID: "xiaomi", Name: "Xiaomi API key", DefaultAPI: "openai-completions", DefaultID: "mimo-v2.5", BaseURL: "https://api.xiaomimimo.com/v1", Env: []string{"XIAOMI_API_KEY"}},
	{ID: "xiaomi-token-plan-cn", Name: "Xiaomi Token Plan CN API key", DefaultAPI: "openai-completions", DefaultID: "mimo-v2.5", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Env: []string{"XIAOMI_TOKEN_PLAN_CN_API_KEY"}},
	{ID: "xiaomi-token-plan-ams", Name: "Xiaomi Token Plan AMS API key", DefaultAPI: "openai-completions", DefaultID: "mimo-v2.5", BaseURL: "https://token-plan-ams.xiaomimimo.com/v1", Env: []string{"XIAOMI_TOKEN_PLAN_AMS_API_KEY"}},
	{ID: "xiaomi-token-plan-sgp", Name: "Xiaomi Token Plan SGP API key", DefaultAPI: "openai-completions", DefaultID: "mimo-v2.5", BaseURL: "https://token-plan-sgp.xiaomimimo.com/v1", Env: []string{"XIAOMI_TOKEN_PLAN_SGP_API_KEY"}},
	{ID: "zai", Name: "Z.AI API key", DefaultAPI: "openai-completions", DefaultID: "glm-4.7", BaseURL: "https://api.z.ai/api/coding/paas/v4", Env: []string{"ZAI_API_KEY"}},
	{ID: "zai-coding-cn", Name: "Z.AI Coding CN API key", DefaultAPI: "openai-completions", DefaultID: "glm-4.6v", BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4", Env: []string{"ZAI_CODING_CN_API_KEY"}},
	{ID: "xai", Name: "xAI API key", DefaultAPI: "openai-responses", DefaultID: "grok-4.3", BaseURL: "https://api.x.ai/v1", Env: []string{"XAI_API_KEY", "GROK_API_KEY"}},
	{ID: "fireworks", Name: "Fireworks API key", DefaultAPI: "anthropic-messages", DefaultID: "accounts/fireworks/models/deepseek-v4-flash", BaseURL: "https://api.fireworks.ai/inference", Env: []string{"FIREWORKS_API_KEY"}},
	{ID: "opencode-go", Name: "OpenCode API key", DefaultAPI: "openai-completions", DefaultID: "deepseek-v4-flash", BaseURL: "https://opencode.ai/zen/go/v1", Env: []string{"OPENCODE_API_KEY"}},
	{ID: "github-copilot", Name: "GitHub Copilot token", DefaultAPI: "openai-completions", DefaultID: "claude-fable-5", BaseURL: "https://api.individual.githubcopilot.com", Env: []string{"COPILOT_GITHUB_TOKEN"}, Headers: map[string]string{
		"User-Agent":             "GitHubCopilotChat/0.35.0",
		"Editor-Version":         "vscode/1.107.0",
		"Editor-Plugin-Version":  "copilot-chat/0.35.0",
		"Copilot-Integration-Id": "vscode-chat",
	}},
	{ID: "cloudflare-workers-ai", Name: "Cloudflare API key", DefaultAPI: "openai-completions", DefaultID: "@cf/deepseek-ai/deepseek-v4-flash-0731", BaseURL: "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai/v1"},
	{ID: "cloudflare-ai-gateway", Name: "Cloudflare API key", DefaultAPI: "anthropic-messages", DefaultID: "claude-fable-5", BaseURL: "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic"},
	{ID: "azure-openai-responses", Name: "Azure OpenAI API key", DefaultAPI: "azure-openai-responses", DefaultID: "gpt-4"},
	{ID: "google-vertex", Name: "Google Cloud credentials", DefaultAPI: "google-vertex", DefaultID: "gemini-2.5-flash", BaseURL: "https://{location}-aiplatform.googleapis.com", Env: []string{"GOOGLE_CLOUD_API_KEY", "GOOGLE_CLOUD_PROJECT", "GCLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "GOOGLE_APPLICATION_CREDENTIALS"}},
	{ID: "mistral", Name: "Mistral API key", DefaultAPI: "mistral-conversations", DefaultID: "codestral-latest", BaseURL: "https://api.mistral.ai", Env: []string{"MISTRAL_API_KEY"}},
	{ID: "openai-codex", Name: "OpenAI Codex", DefaultAPI: "openai-codex-responses", DefaultID: "gpt-5.3-codex-spark", BaseURL: "https://chatgpt.com/backend-api"},
}

func registerExtraProviders() {
	for _, p := range extraProviders {
		RegisterProvider(ProviderSpec{
			ID:         p.ID,
			Name:       p.Name,
			Env:        p.Env,
			BaseURL:    p.BaseURL,
			DefaultAPI: p.DefaultAPI,
			DefaultID:  p.DefaultID,
			Headers:    p.Headers,
			Models: []Model{{
				Provider: p.ID,
				ID:       p.DefaultID,
				API:      p.DefaultAPI,
			}},
		})
	}
}
