package ai

import "github.com/Lowpower/pigo/internal/models"

// calculateCost fills usage.Cost from catalog rates in dollars per million tokens.
// The tier is the highest cost.Tiers threshold exceeded by uncached input plus
// cache read and cache write. CacheWrite1h is not counted again. The chosen
// rates apply to the whole request. CacheWrite1h is the one-hour subset of
// CacheWrite and is priced at twice the selected input rate. The rest of
// CacheWrite uses the five-minute cache-write rate. A nil catalog cost leaves
// any existing dollar fields unchanged.
func calculateCost(cost *models.Cost, usage *Usage) {
	if cost == nil || usage == nil {
		return
	}
	input, output, cacheRead, cacheWrite := cost.Input, cost.Output, cost.CacheRead, cost.CacheWrite
	total := usage.Input + usage.CacheRead + usage.CacheWrite
	matched := -1
	for _, tier := range cost.Tiers {
		if total > tier.InputTokensAbove && tier.InputTokensAbove > matched {
			input, output, cacheRead, cacheWrite = tier.Input, tier.Output, tier.CacheRead, tier.CacheWrite
			matched = tier.InputTokensAbove
		}
	}
	longWrite := usage.CacheWrite1h
	shortWrite := usage.CacheWrite - longWrite
	usage.Cost.Input = tokenPrice(usage.Input, input)
	usage.Cost.Output = tokenPrice(usage.Output, output)
	usage.Cost.CacheRead = tokenPrice(usage.CacheRead, cacheRead)
	usage.Cost.CacheWrite = (cacheWrite*float64(shortWrite) + input*2*float64(longWrite)) / 1_000_000
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
}

func tokenPrice(tokens int, perMillion float64) float64 {
	return float64(tokens) * perMillion / 1_000_000
}

// applyInclusivePromptUsage splits a prompt count that already includes cache
// reads and writes. Input keeps only the uncached portion. TotalTokens is the
// sum of input, output, cache read, and cache write.
func applyInclusivePromptUsage(dst *Usage, prompt, output, cacheRead, cacheWrite int) {
	if dst == nil {
		return
	}
	if cacheRead < 0 {
		cacheRead = 0
	}
	if cacheWrite < 0 {
		cacheWrite = 0
	}
	input := prompt - cacheRead - cacheWrite
	if input < 0 {
		input = 0
	}
	dst.Input = input
	dst.Output = output
	dst.CacheRead = cacheRead
	dst.CacheWrite = cacheWrite
	dst.TotalTokens = input + output + cacheRead + cacheWrite
}

// applyServiceTierCost multiplies a catalog cost when OpenAI served the
// request on the fast or priority tier. Missing, default, and auto stay at
// the catalog rate. Fast uses the same markup as priority.
func applyServiceTierCost(usage *Usage, tier string) {
	if usage == nil {
		return
	}
	switch tier {
	case "fast", "priority":
		usage.Cost.Input *= 2
		usage.Cost.Output *= 2
		usage.Cost.CacheRead *= 2
		usage.Cost.CacheWrite *= 2
		usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
	}
}

func catalogCost(opts Options) *models.Cost {
	m, ok := models.Lookup(opts.Provider, opts.Model)
	if !ok {
		return nil
	}
	return m.Cost
}
