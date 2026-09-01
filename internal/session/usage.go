package session

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
)

const cacheMissNoiseFloor = 1024

func usageCostBreakdown(entries []Entry) []CostBreakdown {
	totals := map[string]struct {
		cost   float64
		tokens int
	}{}
	add := func(key string, u ai.Usage) {
		cur := totals[key]
		cur.cost += u.Cost.Total
		cur.tokens += u.Input + u.Output + u.CacheRead + u.CacheWrite
		totals[key] = cur
	}
	for _, e := range entries {
		switch e.Type {
		case "compaction", "branch_summary":
			if e.Usage != nil {
				add("Tools/summaries", *e.Usage)
			}
		case "message", "":
			var payload struct {
				Role     string    `json:"role"`
				Provider string    `json:"provider"`
				Model    string    `json:"model"`
				Usage    *ai.Usage `json:"usage"`
			}
			if json.Unmarshal(e.Message, &payload) != nil {
				continue
			}
			switch payload.Role {
			case "assistant":
				var am ai.AssistantMessage
				if json.Unmarshal(e.Message, &am) != nil {
					continue
				}
				key := am.Provider + "/" + am.Model
				if am.Provider == "" && am.Model == "" {
					key = "unknown"
				}
				add(key, am.Usage)
			case "toolResult", "tool":
				if payload.Usage != nil {
					add("Tools/summaries", *payload.Usage)
				}
			}
		}
	}
	out := make([]CostBreakdown, 0, len(totals))
	for key, t := range totals {
		if t.cost <= 0 && t.tokens <= 0 {
			continue
		}
		out = append(out, CostBreakdown{Key: key, Cost: t.cost, Tokens: t.tokens})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cost > out[j].Cost })
	return out
}

type previousRequest struct {
	promptTokens  int
	modelKey      string
	reportedCache bool
}

func computeCacheWaste(entries []Entry) CacheWaste {
	var prev *previousRequest
	var waste CacheWaste
	for _, e := range entries {
		if e.Type == "compaction" || e.Type == "branch_summary" {
			prev = nil
			continue
		}
		if e.Type != "message" && e.Type != "" {
			continue
		}
		var am ai.AssistantMessage
		if json.Unmarshal(e.Message, &am) != nil || am.Role != ai.RoleAssistant {
			continue
		}
		if miss := detectCacheMiss(prev, am); miss != nil {
			waste.MissedTokens += miss.missedTokens
			waste.MissedCost += miss.missedCost
			waste.MissCount++
		}
		if next := asPreviousRequest(am, prev); next != nil {
			prev = next
		}
	}
	return waste
}

type cacheMiss struct {
	missedTokens int
	missedCost   float64
}

func detectCacheMiss(prev *previousRequest, message ai.AssistantMessage) *cacheMiss {
	u := message.Usage
	promptTokens := u.Input + u.CacheRead + u.CacheWrite
	if prev == nil || promptTokens <= 0 || (u.CacheRead+u.CacheWrite == 0 && !prev.reportedCache) {
		return nil
	}
	missed := prev.promptTokens
	if promptTokens < missed {
		missed = promptTokens
	}
	missed -= u.CacheRead
	if missed <= cacheMissNoiseFloor {
		return nil
	}
	paidTokens := u.Input + u.CacheWrite
	paidPerToken := 0.0
	if paidTokens > 0 {
		paidPerToken = (u.Cost.Input + u.Cost.CacheWrite) / float64(paidTokens)
	}
	readPerToken := 0.0
	if u.CacheRead > 0 {
		readPerToken = u.Cost.CacheRead / float64(u.CacheRead)
	} else {
		readPerToken = models.CacheReadPerToken(message.Provider, message.Model)
	}
	diff := paidPerToken - readPerToken
	if diff < 0 {
		diff = 0
	}
	return &cacheMiss{missedTokens: missed, missedCost: float64(missed) * diff}
}

func asPreviousRequest(message ai.AssistantMessage, prev *previousRequest) *previousRequest {
	u := message.Usage
	promptTokens := u.Input + u.CacheRead + u.CacheWrite
	if promptTokens <= 0 {
		return nil
	}
	reported := u.CacheRead+u.CacheWrite > 0
	if prev != nil {
		reported = reported || prev.reportedCache
	}
	return &previousRequest{
		promptTokens:  promptTokens,
		modelKey:      message.Provider + "/" + message.Model,
		reportedCache: reported,
	}
}

// LastCacheMissNotice is the transcript line for the latest counted cache miss.
func LastCacheMissNotice(entries []Entry) string {
	var prev *previousRequest
	var last *cacheMiss
	for _, e := range entries {
		if e.Type == "compaction" || e.Type == "branch_summary" {
			prev = nil
			continue
		}
		if e.Type != "message" && e.Type != "" {
			continue
		}
		var am ai.AssistantMessage
		if json.Unmarshal(e.Message, &am) != nil || am.Role != ai.RoleAssistant {
			continue
		}
		if miss := detectCacheMiss(prev, am); miss != nil {
			last = miss
		}
		if next := asPreviousRequest(am, prev); next != nil {
			prev = next
		}
	}
	if last == nil {
		return ""
	}
	if last.missedCost >= 0.0001 {
		return fmt.Sprintf("Cache miss: %d tokens re-billed ($%.3f)", last.missedTokens, last.missedCost)
	}
	return fmt.Sprintf("Cache miss: %d tokens re-billed", last.missedTokens)
}
