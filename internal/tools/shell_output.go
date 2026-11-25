package tools

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

// StructuredSink collects JSON a tool records beside its text result.
// tools does not import ext: ext tests import agent, and agent imports tools.
type StructuredSink struct {
	Raw json.RawMessage
}

type structuredKey struct{}

// WithStructuredSink returns ctx carrying sink. A nil sink leaves ctx unchanged.
func WithStructuredSink(ctx context.Context, sink *StructuredSink) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, structuredKey{}, sink)
}

func structuredSink(ctx context.Context) *StructuredSink {
	if ctx == nil {
		return nil
	}
	sink, _ := ctx.Value(structuredKey{}).(*StructuredSink)
	return sink
}

// shellStructured is the pi v1.3.0 bash/powershell output schema.
type shellStructured struct {
	Output          string  `json:"output"`
	Truncated       bool    `json:"truncated"`
	FullOutputPath  string  `json:"full_output_path,omitempty"`
	ExitCode        int     `json:"exit_code"`
	WallTimeSeconds float64 `json:"wall_time_seconds"`
}

func shellOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"output": map[string]any{
				"type":        "string",
				"description": "Combined stdout and stderr, possibly truncated",
			},
			"truncated": map[string]any{"type": "boolean"},
			"full_output_path": map[string]any{
				"type":        "string",
				"description": "Full output, when truncated",
			},
			"exit_code":         map[string]any{"type": "number"},
			"wall_time_seconds": map[string]any{"type": "number"},
		},
		"required": []any{"output", "truncated", "exit_code", "wall_time_seconds"},
	}
}

// wallSeconds rounds the duration already shown as Took to a tenth of a second.
func wallSeconds(d time.Duration) float64 {
	if d < 0 {
		d = 0
	}
	return math.Round(float64(d)/float64(time.Second)*10) / 10
}

// storeShellStructured records the programmatic result when the command
// finished with an exit code. full_output_path is set only when that result
// itself was cut at 1 MiB.
func storeShellStructured(ctx context.Context, full, path, tempPrefix string, exitCode int, elapsed time.Duration) {
	box := structuredSink(ctx)
	if box == nil {
		return
	}
	content, truncated := structuredShellOutput(full)
	if truncated && path == "" {
		if p, err := persistFullOutput(tempPrefix, full); err == nil {
			path = p
		}
	}
	payload := shellStructured{
		Output:          content,
		Truncated:       truncated,
		ExitCode:        exitCode,
		WallTimeSeconds: wallSeconds(elapsed),
	}
	if truncated && path != "" {
		payload.FullOutputPath = path
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	box.Raw = raw
}
