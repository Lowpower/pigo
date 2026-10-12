package ai

import "github.com/Lowpower/pigo/internal/models"

// calculateCost fills usage.Cost from catalog rates in dollars per million tokens.
// CacheWrite1h is the one-hour subset of CacheWrite and is priced at twice the
// input rate. The rest of CacheWrite uses the five-minute cache-write rate.
// A nil catalog cost leaves any existing dollar fields unchanged.
func calculateCost(cost *models.Cost, usage *Usage) {
	if cost == nil || usage == nil {
		return
	}
	longWrite := usage.CacheWrite1h
	shortWrite := usage.CacheWrite - longWrite
	usage.Cost.Input = tokenPrice(usage.Input, cost.Input)
	usage.Cost.Output = tokenPrice(usage.Output, cost.Output)
	usage.Cost.CacheRead = tokenPrice(usage.CacheRead, cost.CacheRead)
	usage.Cost.CacheWrite = (cost.CacheWrite*float64(shortWrite) + cost.Input*2*float64(longWrite)) / 1_000_000
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
