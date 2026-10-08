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

func catalogCost(opts Options) *models.Cost {
	m, ok := models.Lookup(opts.Provider, opts.Model)
	if !ok {
		return nil
	}
	return m.Cost
}
