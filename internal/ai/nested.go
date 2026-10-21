package ai

// NestedToolCall is one bounded record of a call made through executeTool.
type NestedToolCall struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Status         string         `json:"status"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	ArgumentsBytes int            `json:"argumentsBytes,omitempty"`
	DurationMs     int            `json:"durationMs,omitempty"`
	Error          string         `json:"error,omitempty"`
}

// NestedToolCalls is the record stored on the calling tool's result.
// Complete is false when a call was dropped or its arguments were omitted.
type NestedToolCalls struct {
	Calls    []NestedToolCall `json:"calls"`
	Complete bool             `json:"complete"`
}

// AddUsage returns the sum of two usage values.
func AddUsage(a, b Usage) Usage {
	a.Input += b.Input
	a.Output += b.Output
	a.CacheRead += b.CacheRead
	a.CacheWrite += b.CacheWrite
	a.CacheWrite1h += b.CacheWrite1h
	a.Reasoning += b.Reasoning
	a.TotalTokens += b.TotalTokens
	a.Cost.Input += b.Cost.Input
	a.Cost.Output += b.Cost.Output
	a.Cost.CacheRead += b.Cost.CacheRead
	a.Cost.CacheWrite += b.Cost.CacheWrite
	a.Cost.Total += b.Cost.Total
	return a
}
