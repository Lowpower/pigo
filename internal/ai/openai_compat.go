package ai

import (
	"strings"

	"github.com/Lowpower/pigo/internal/models"
)

const minAnswerTokens = 1024

// resolvedOpenAICompat is the provider and baseUrl default after explicit model.compat overrides.
type resolvedOpenAICompat struct {
	supportsStore                    bool
	supportsFinishReason             bool
	supportsUsageInStreaming         bool
	supportsDeveloperRole            bool
	maxTokensField                   string
	requiresToolResultName           bool
	requiresAssistantAfterToolResult bool
	requiresThinkingAsText           bool
	requiresReasoningContent         bool
	openRouterRouting                map[string]any
	vercel                           *models.VercelGatewayRouting
	zaiToolStream                    bool
	thinkingBudgetField              string
	cacheControlFormat               string
	model                            *models.Model
}

func (c resolvedOpenAICompat) developerRole() bool {
	return c.model != nil && c.model.SupportsReasoning() && c.supportsDeveloperRole
}

func (c resolvedOpenAICompat) emptyReasoningContent() bool {
	if !c.requiresReasoningContent {
		return false
	}
	if c.model == nil {
		return true
	}
	return c.model.SupportsReasoning()
}

func resolveOpenAICompat(opts Options, baseURL string) resolvedOpenAICompat {
	var model *models.Model
	catalogURL := ""
	if m, ok := models.Lookup(opts.Provider, opts.Model); ok {
		copied := m
		model = &copied
		catalogURL = m.BaseURL
	}
	effective := strings.TrimSpace(baseURL)
	if effective == "" {
		effective = catalogURL
	}
	out := detectOpenAICompat(opts.Provider, opts.Model, effective)
	out.model = model
	if model == nil || model.Compat == nil {
		return out
	}
	c := model.Compat
	out.supportsStore = boolOr(c.SupportsStore, out.supportsStore)
	out.supportsFinishReason = boolOr(c.SupportsFinishReason, out.supportsFinishReason)
	out.supportsUsageInStreaming = boolOr(c.SupportsUsageInStreaming, out.supportsUsageInStreaming)
	out.supportsDeveloperRole = boolOr(c.SupportsDeveloperRole, out.supportsDeveloperRole)
	if field := strings.TrimSpace(c.MaxTokensField); field != "" {
		out.maxTokensField = field
	}
	out.requiresToolResultName = boolOr(c.RequiresToolResultName, out.requiresToolResultName)
	out.requiresAssistantAfterToolResult = boolOr(c.RequiresAssistantAfterToolResult, out.requiresAssistantAfterToolResult)
	out.requiresThinkingAsText = boolOr(c.RequiresThinkingAsText, out.requiresThinkingAsText)
	out.requiresReasoningContent = boolOr(c.RequiresReasoningContentOnAssistantMessages, out.requiresReasoningContent)
	if c.OpenRouterRouting != nil {
		out.openRouterRouting = c.OpenRouterRouting
	}
	if c.VercelGatewayRouting != nil {
		out.vercel = c.VercelGatewayRouting
	}
	out.zaiToolStream = boolOr(c.ZaiToolStream, out.zaiToolStream)
	out.thinkingBudgetField = thinkingBudgetField(c)
	if format := strings.TrimSpace(c.CacheControlFormat); format != "" {
		out.cacheControlFormat = format
	}
	return out
}

func boolOr(explicit *bool, detected bool) bool {
	if explicit != nil {
		return *explicit
	}
	return detected
}

func thinkingBudgetField(c *models.Compat) string {
	if c == nil {
		return ""
	}
	if field := strings.TrimSpace(c.ThinkingTokenBudgetField); field != "" {
		return field
	}
	if c.SupportsThinkingTokenBudget != nil && *c.SupportsThinkingTokenBudget {
		return "thinking_token_budget"
	}
	return ""
}

func detectOpenAICompat(provider, modelID, baseURL string) resolvedOpenAICompat {
	p := strings.ToLower(strings.TrimSpace(provider))
	u := strings.ToLower(strings.TrimSpace(baseURL))
	isZai := p == "zai" || p == "zai-coding-cn" || strings.Contains(u, "api.z.ai") || strings.Contains(u, "open.bigmodel.cn")
	isTogether := p == "together" || strings.Contains(u, "api.together.ai") || strings.Contains(u, "api.together.xyz")
	isMoonshot := p == "moonshotai" || p == "moonshotai-cn" || strings.Contains(u, "api.moonshot.")
	isOpenRouter := p == "openrouter" || strings.Contains(u, "openrouter.ai")
	isCloudflareWorkers := p == "cloudflare-workers-ai" || strings.Contains(u, "api.cloudflare.com")
	isCloudflareGateway := p == "cloudflare-ai-gateway" || strings.Contains(u, "gateway.ai.cloudflare.com")
	isNvidia := p == "nvidia" || strings.Contains(u, "integrate.api.nvidia.com")
	isAntLing := p == "ant-ling" || strings.Contains(u, "api.ant-ling.com")
	isCerebras := p == "cerebras" || strings.Contains(u, "cerebras.ai")
	isDeepSeek := p == "deepseek" || strings.Contains(u, "deepseek.com")
	isXAI := p == "xai" || strings.Contains(u, "api.x.ai")
	isOpenCode := p == "opencode" || strings.Contains(u, "opencode.ai")
	isChutes := strings.Contains(u, "chutes.ai")

	k3Style := kimiCodingDeferredTools(p, modelID)
	nonStandard := isNvidia || isCerebras || isXAI || isTogether || isChutes || isDeepSeek || isZai || isMoonshot || isOpenCode || isCloudflareWorkers || isCloudflareGateway || isAntLing || k3Style
	useMaxTokens := isChutes || isDeepSeek || isMoonshot || isCloudflareGateway || isTogether || isNvidia || isAntLing || isZai || k3Style

	field := "max_completion_tokens"
	if useMaxTokens {
		field = "max_tokens"
	}
	developer := false
	if isOpenRouter && (strings.HasPrefix(modelID, "anthropic/") || strings.HasPrefix(modelID, "openai/")) {
		developer = true
	} else if !nonStandard && !isOpenRouter {
		developer = true
	}
	cache := ""
	if p == "openrouter" && strings.HasPrefix(modelID, "anthropic/") {
		cache = "anthropic"
	}
	return resolvedOpenAICompat{
		supportsStore:            !nonStandard,
		supportsFinishReason:     true,
		supportsUsageInStreaming: true,
		supportsDeveloperRole:    developer,
		maxTokensField:           field,
		requiresReasoningContent: isDeepSeek || k3Style,
		cacheControlFormat:       cache,
	}
}

func clampedThinkingBudget(opts Options, model *models.Model, ceiling int) int {
	if model == nil || !model.SupportsReasoning() || reasoningEffort(opts) == "" || opts.ThinkingBudget <= 0 {
		return 0
	}
	if ceiling <= 0 {
		ceiling = model.MaxTokens
	}
	room := ceiling - minAnswerTokens
	if room < 0 {
		room = 0
	}
	budget := opts.ThinkingBudget
	if budget > room {
		budget = room
	}
	if budget <= 0 {
		return 0
	}
	return budget
}

func vercelGatewayOptions(v *models.VercelGatewayRouting) map[string]any {
	if v == nil || (len(v.Only) == 0 && len(v.Order) == 0) {
		return nil
	}
	gateway := map[string]any{}
	if len(v.Only) > 0 {
		gateway["only"] = v.Only
	}
	if len(v.Order) > 0 {
		gateway["order"] = v.Order
	}
	return gateway
}

func applyOpenAICacheControl(msgs []map[string]any, tools []map[string]any, opts Options, c resolvedOpenAICompat) {
	cc := map[string]any{"type": "ephemeral"}
	var compat *models.Compat
	if c.model != nil {
		compat = c.model.Compat
	}
	if strings.EqualFold(strings.TrimSpace(opts.CacheRetention), "long") && longCacheOK(compat) {
		cc["ttl"] = "1h"
	}
	for _, msg := range msgs {
		role, _ := msg["role"].(string)
		if role == "system" || role == "developer" {
			_ = addOpenAICacheControl(msg, cc)
			break
		}
	}
	if len(tools) > 0 {
		tools[len(tools)-1]["cache_control"] = cloneCacheControl(cc)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		role, _ := msgs[i]["role"].(string)
		if role != "user" && role != "assistant" && role != "tool" {
			continue
		}
		if addOpenAICacheControl(msgs[i], cc) {
			return
		}
	}
}

func addOpenAICacheControl(msg map[string]any, cc map[string]any) bool {
	switch content := msg["content"].(type) {
	case string:
		if content == "" {
			return false
		}
		msg["content"] = []map[string]any{{
			"type":          "text",
			"text":          content,
			"cache_control": cloneCacheControl(cc),
		}}
		return true
	case []map[string]any:
		for i := len(content) - 1; i >= 0; i-- {
			if content[i]["type"] == "text" {
				content[i]["cache_control"] = cloneCacheControl(cc)
				return true
			}
		}
	}
	return false
}

func cloneCacheControl(cc map[string]any) map[string]any {
	out := make(map[string]any, len(cc))
	for k, v := range cc {
		out[k] = v
	}
	return out
}
