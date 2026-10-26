package ai

import (
	"math"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestCalculateCostUsesBaseRatesWithoutTiers(t *testing.T) {
	cost := &models.Cost{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}
	usage := Usage{Input: 1_000_000, Output: 500_000, CacheRead: 200_000, CacheWrite: 100_000}
	calculateCost(cost, &usage)
	want := UsageCost{Input: 3, Output: 7.5, CacheRead: 0.06, CacheWrite: 0.375, Total: 10.935}
	assertUsageCost(t, usage.Cost, want)
}

func TestCalculateCostSelectsPromptLengthTier(t *testing.T) {
	cost := &models.Cost{
		Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 1.25,
		Tiers: []models.CostTier{
			{InputTokensAbove: 100_000, Input: 5, Output: 8, CacheRead: 0.5, CacheWrite: 6},
			{InputTokensAbove: 200_000, Input: 9, Output: 12, CacheRead: 0.9, CacheWrite: 10},
		},
	}

	atBoundary := Usage{Input: 100_000, Output: 1_000_000}
	calculateCost(cost, &atBoundary)
	assertUsageCost(t, atBoundary.Cost, UsageCost{Input: 0.1, Output: 2, Total: 2.1})

	justOver := Usage{Input: 100_001, Output: 1_000_000}
	calculateCost(cost, &justOver)
	assertUsageCost(t, justOver.Cost, UsageCost{Input: 0.500005, Output: 8, Total: 8.500005})

	mid := Usage{Input: 200_000, Output: 0}
	calculateCost(cost, &mid)
	assertUsageCost(t, mid.Cost, UsageCost{Input: 1, Total: 1})

	long := Usage{Input: 200_001, Output: 0}
	calculateCost(cost, &long)
	assertUsageCost(t, long.Cost, UsageCost{Input: 1.800009, Total: 1.800009})
}

func TestCalculateCostCountsCacheTowardTierAndKeepsFirstEqualThreshold(t *testing.T) {
	cost := &models.Cost{
		Input: 1, Output: 1, CacheRead: 0.2, CacheWrite: 1,
		Tiers: []models.CostTier{
			{InputTokensAbove: 100_000, Input: 4, Output: 4, CacheRead: 0.8, CacheWrite: 4},
			{InputTokensAbove: 100_000, Input: 9, Output: 9, CacheRead: 9, CacheWrite: 9},
		},
	}
	under := Usage{Input: 50_000, CacheRead: 40_000, CacheWrite: 10_000}
	calculateCost(cost, &under)
	assertUsageCost(t, under.Cost, UsageCost{Input: 0.05, CacheRead: 0.008, CacheWrite: 0.01, Total: 0.068})

	over := Usage{Input: 50_000, CacheRead: 40_000, CacheWrite: 10_001, CacheWrite1h: 10_001}
	calculateCost(cost, &over)
	// Equal thresholds keep the first tier. CacheWrite1h is not added again.
	// The one-hour subset uses 2x the selected tier input (4), not the base input.
	assertUsageCost(t, over.Cost, UsageCost{Input: 0.2, CacheRead: 0.032, CacheWrite: 0.080008, Total: 0.312008})
}

func TestCalculateCostOneHourWriteUsesSelectedTierInput(t *testing.T) {
	cost := &models.Cost{
		Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 1.25,
		Tiers: []models.CostTier{
			{InputTokensAbove: 100_000, Input: 5, Output: 8, CacheRead: 0.5, CacheWrite: 6},
		},
	}
	usage := Usage{CacheWrite: 100_001, CacheWrite1h: 40_000}
	calculateCost(cost, &usage)
	// short 60001 * 6 + long 40000 * 5 * 2, per million
	wantWrite := (6*float64(60_001) + 5*2*float64(40_000)) / 1_000_000
	assertUsageCost(t, usage.Cost, UsageCost{CacheWrite: wantWrite, Total: wantWrite})
}

func TestCalculateCostDoesNotInheritMissingTierRates(t *testing.T) {
	cost := &models.Cost{
		Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75,
		Tiers: []models.CostTier{{InputTokensAbove: 10, Input: 6, Output: 20}},
	}
	usage := Usage{Input: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000}
	calculateCost(cost, &usage)
	assertUsageCost(t, usage.Cost, UsageCost{Input: 6, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 6})
}

func assertUsageCost(t *testing.T, got, want UsageCost) {
	t.Helper()
	if math.Abs(got.Input-want.Input) > 1e-9 || math.Abs(got.Output-want.Output) > 1e-9 ||
		math.Abs(got.CacheRead-want.CacheRead) > 1e-9 || math.Abs(got.CacheWrite-want.CacheWrite) > 1e-9 ||
		math.Abs(got.Total-want.Total) > 1e-9 {
		t.Fatalf("cost = %+v, want %+v", got, want)
	}
}
