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
	// Name is the login/API-key prompt label; empty uses "<id> API key".
	Name string
	// Headers are default request headers merged in StreamFor (caller wins).
	Headers map[string]string
	// FilterModels optionally restricts catalog (e.g. GitHub Copilot).
	FilterModels func([]Model) []Model
	// RefreshModels optionally pulls a remote/dynamic catalog into store.
	RefreshModels func(store CatalogStore) error
	// RadiusGateway is the models.json oauth:radius gateway.
	// Empty on the built-in radius provider, which reads RadiusGateway() instead.
	RadiusGateway string
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

// UnregisterProvider removes a dynamically registered provider.
func UnregisterProvider(id string) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, ok := providers[id]; !ok {
		return
	}
	delete(providers, id)
	out := regOrder[:0]
	for _, existing := range regOrder {
		if existing != id {
			out = append(out, existing)
		}
	}
	regOrder = out
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

// MaxTokens is the catalog output cap, or 0 when unknown.
func MaxTokens(provider, id string) int {
	m, ok := Lookup(provider, id)
	if !ok || m.MaxTokens <= 0 {
		return 0
	}
	return m.MaxTokens
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
	list = mergeOverlay(providerID, list, remoteOverlay(providerID))
	list = mergeOverlay(providerID, list, userOverlay(providerID))
	return list
}

func mergeOverlay(providerID string, base, extra []Model) []Model {
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
			if m.Cost != nil {
				out[i].Cost = m.Cost
			}
			if m.MaxTokens > 0 {
				out[i].MaxTokens = m.MaxTokens
			}
			if m.PromptCache != nil {
				out[i].PromptCache = m.PromptCache
			}
			if m.Compat != nil {
				out[i].Compat = mergeCompat(out[i].Compat, m.Compat)
			}
			if m.InputLimits != nil {
				out[i].InputLimits = mergeInputLimits(out[i].InputLimits, m.InputLimits)
			}
			if m.Name != "" {
				out[i].Name = m.Name
			}
			if m.ContextWindow > 0 {
				out[i].ContextWindow = m.ContextWindow
			}
			if m.Reasoning != nil {
				out[i].Reasoning = m.Reasoning
			}
			if len(m.Input) > 0 {
				out[i].Input = m.Input
			}
			if len(m.ThinkingLevelMap) > 0 {
				out[i].ThinkingLevelMap = m.ThinkingLevelMap
			}
			if len(m.SamplingParams) > 0 {
				out[i].SamplingParams = MergeSamplingParams(out[i].SamplingParams, m.SamplingParams)
			}
			out[i] = fillOpus55ThinkingLevels(out[i])
			continue
		}
		m.SamplingParams = MergeSamplingParams(nil, m.SamplingParams)
		if m.Provider == "" {
			m.Provider = providerID
		}
		if len(m.ThinkingLevelMap) == 0 {
			m = applyOpenRouterReasoning(m, false)
		} else {
			m = ensureOpenRouterFormat(m)
		}
		m = fillOpus55ThinkingLevels(m)
		idx[m.ID] = len(out)
		out = append(out, m)
	}
	return out
}

// MergeSamplingParams overlays extra onto base, one key at a time.
// An empty extra leaves base unchanged. The result is nil when nothing is set.
// Zero, false, and null values are kept.
func MergeSamplingParams(base, extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return cloneSamplingParams(base)
	}
	out := cloneSamplingParams(base)
	if out == nil {
		out = make(map[string]any, len(extra))
	}
	for k, v := range extra {
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneSamplingParams(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeCompat(base, extra *Compat) *Compat {
	if extra == nil {
		return base
	}
	if base == nil {
		c := *extra
		cloneCompatPointers(&c, extra)
		return &c
	}
	out := *base
	if extra.SupportsStrictMode != nil {
		out.SupportsStrictMode = cloneBool(extra.SupportsStrictMode)
	}
	if extra.ForceAdaptiveThinking != nil {
		out.ForceAdaptiveThinking = cloneBool(extra.ForceAdaptiveThinking)
	}
	if extra.AllowedFallbackModels != nil {
		out.AllowedFallbackModels = cloneFallbacks(extra.AllowedFallbackModels)
	}
	mergeOpenAICompat(&out, extra)
	return &out
}

// cloneCompatPointers copies pointer and map fields so the result does not
// alias the overlay value. Callers pass the struct that was copied from extra.
func cloneCompatPointers(out, extra *Compat) {
	out.SupportsStrictMode = cloneBool(extra.SupportsStrictMode)
	out.ForceAdaptiveThinking = cloneBool(extra.ForceAdaptiveThinking)
	out.AllowedFallbackModels = cloneFallbacks(extra.AllowedFallbackModels)
	out.SupportsStore = cloneBool(extra.SupportsStore)
	out.SupportsUsageInStreaming = cloneBool(extra.SupportsUsageInStreaming)
	out.SupportsDeveloperRole = cloneBool(extra.SupportsDeveloperRole)
	out.RequiresToolResultName = cloneBool(extra.RequiresToolResultName)
	out.RequiresAssistantAfterToolResult = cloneBool(extra.RequiresAssistantAfterToolResult)
	out.RequiresThinkingAsText = cloneBool(extra.RequiresThinkingAsText)
	out.RequiresReasoningContentOnAssistantMessages = cloneBool(extra.RequiresReasoningContentOnAssistantMessages)
	out.OpenRouterRouting = cloneAnyMap(extra.OpenRouterRouting)
	out.VercelGatewayRouting = cloneVercelRouting(extra.VercelGatewayRouting)
	out.ZaiToolStream = cloneBool(extra.ZaiToolStream)
	out.SupportsThinkingTokenBudget = cloneBool(extra.SupportsThinkingTokenBudget)
}

// mergeOpenAICompat overlays Chat Completions switches that extra sets.
// An unset pointer, empty string, or nil map leaves the catalog value.
func mergeOpenAICompat(out, extra *Compat) {
	if extra.SupportsStore != nil {
		out.SupportsStore = cloneBool(extra.SupportsStore)
	}
	if extra.SupportsUsageInStreaming != nil {
		out.SupportsUsageInStreaming = cloneBool(extra.SupportsUsageInStreaming)
	}
	if extra.SupportsDeveloperRole != nil {
		out.SupportsDeveloperRole = cloneBool(extra.SupportsDeveloperRole)
	}
	if extra.MaxTokensField != "" {
		out.MaxTokensField = extra.MaxTokensField
	}
	if extra.RequiresToolResultName != nil {
		out.RequiresToolResultName = cloneBool(extra.RequiresToolResultName)
	}
	if extra.RequiresAssistantAfterToolResult != nil {
		out.RequiresAssistantAfterToolResult = cloneBool(extra.RequiresAssistantAfterToolResult)
	}
	if extra.RequiresThinkingAsText != nil {
		out.RequiresThinkingAsText = cloneBool(extra.RequiresThinkingAsText)
	}
	if extra.RequiresReasoningContentOnAssistantMessages != nil {
		out.RequiresReasoningContentOnAssistantMessages = cloneBool(extra.RequiresReasoningContentOnAssistantMessages)
	}
	if extra.OpenRouterRouting != nil {
		out.OpenRouterRouting = cloneAnyMap(extra.OpenRouterRouting)
	}
	if extra.VercelGatewayRouting != nil {
		out.VercelGatewayRouting = cloneVercelRouting(extra.VercelGatewayRouting)
	}
	if extra.ZaiToolStream != nil {
		out.ZaiToolStream = cloneBool(extra.ZaiToolStream)
	}
	if extra.ThinkingTokenBudgetField != "" {
		out.ThinkingTokenBudgetField = extra.ThinkingTokenBudgetField
	}
	if extra.SupportsThinkingTokenBudget != nil {
		out.SupportsThinkingTokenBudget = cloneBool(extra.SupportsThinkingTokenBudget)
	}
	if extra.CacheControlFormat != "" {
		out.CacheControlFormat = extra.CacheControlFormat
	}
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneVercelRouting(in *VercelGatewayRouting) *VercelGatewayRouting {
	if in == nil {
		return nil
	}
	out := *in
	if in.Only != nil {
		out.Only = append([]string(nil), in.Only...)
	}
	if in.Order != nil {
		out.Order = append([]string(nil), in.Order...)
	}
	return &out
}

func mergeInputLimits(base, extra *InputLimits) *InputLimits {
	if extra == nil {
		return base
	}
	var baseImages *ImageInputLimits
	if base != nil {
		baseImages = base.Images
	}
	out := &InputLimits{}
	if extra.Images != nil {
		out.Images = mergeImageInputLimits(baseImages, extra.Images)
		return out
	}
	out.Images = cloneImageInputLimits(baseImages)
	return out
}

func mergeImageInputLimits(base, extra *ImageInputLimits) *ImageInputLimits {
	if extra == nil {
		return cloneImageInputLimits(base)
	}
	var baseResize *ImageResize
	if base != nil {
		baseResize = base.Resize
	}
	out := &ImageInputLimits{}
	if extra.Resize != nil {
		out.Resize = mergeImageResize(baseResize, extra.Resize)
		return out
	}
	out.Resize = cloneImageResize(baseResize)
	return out
}

func mergeImageResize(base, extra *ImageResize) *ImageResize {
	if extra == nil {
		return cloneImageResize(base)
	}
	if base == nil {
		return cloneImageResize(extra)
	}
	out := ImageResize{
		MaxWidth:    cloneInt(base.MaxWidth),
		MaxHeight:   cloneInt(base.MaxHeight),
		MaxBytes:    cloneInt(base.MaxBytes),
		JPEGQuality: cloneInt(base.JPEGQuality),
	}
	if extra.MaxWidth != nil {
		out.MaxWidth = cloneInt(extra.MaxWidth)
	}
	if extra.MaxHeight != nil {
		out.MaxHeight = cloneInt(extra.MaxHeight)
	}
	if extra.MaxBytes != nil {
		out.MaxBytes = cloneInt(extra.MaxBytes)
	}
	if extra.JPEGQuality != nil {
		out.JPEGQuality = cloneInt(extra.JPEGQuality)
	}
	return &out
}

func cloneImageInputLimits(in *ImageInputLimits) *ImageInputLimits {
	if in == nil {
		return nil
	}
	return &ImageInputLimits{Resize: cloneImageResize(in.Resize)}
}

func cloneImageResize(in *ImageResize) *ImageResize {
	if in == nil {
		return nil
	}
	return &ImageResize{
		MaxWidth:    cloneInt(in.MaxWidth),
		MaxHeight:   cloneInt(in.MaxHeight),
		MaxBytes:    cloneInt(in.MaxBytes),
		JPEGQuality: cloneInt(in.JPEGQuality),
	}
}

func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}

func cloneBool(v *bool) *bool {
	if v == nil {
		return nil
	}
	b := *v
	return &b
}

func cloneFallbacks(in []FallbackModel) []FallbackModel {
	if in == nil {
		return nil
	}
	out := make([]FallbackModel, len(in))
	copy(out, in)
	return out
}
