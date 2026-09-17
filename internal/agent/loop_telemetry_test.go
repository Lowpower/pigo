package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/telemetry"
)

type captureExporter struct {
	mu      sync.Mutex
	records []telemetry.SpanRecord
}

func (c *captureExporter) Export(records []telemetry.SpanRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, records...)
}

func (c *captureExporter) Shutdown(context.Context) error { return nil }

func (c *captureExporter) snapshot() []telemetry.SpanRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]telemetry.SpanRecord(nil), c.records...)
}

func (c *captureExporter) names() []string {
	recs := c.snapshot()
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Name
	}
	return out
}

func TestLoopEmitsNestedSpans(t *testing.T) {
	telemetry.Reset()
	t.Cleanup(telemetry.Reset)
	sink := &captureExporter{}
	telemetry.AddExporter(sink)

	provider := scriptedProvider(
		toolCallMessage("tc1", "read", map[string]any{"path": "README.md"}),
		textMessage("Done."),
	)
	exec := ToolFunc(func(_ context.Context, _ ToolCall) (string, bool) {
		return "ok", false
	})
	reqCtx := ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "read"}}}
	_ = Run(context.Background(), provider, reqCtx, exec, Config{Model: "test"}).Collect()

	names := sink.names()
	want := []string{"pigo.llm_request", "pigo.tool", "pigo.turn", "pigo.llm_request", "pigo.turn", "pigo.run"}
	if len(names) != len(want) {
		t.Fatalf("spans = %v, want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("spans = %v, want %v", names, want)
		}
	}
	var run, turn, tool telemetry.SpanRecord
	for _, r := range sink.snapshot() {
		switch r.Name {
		case "pigo.run":
			run = r
		case "pigo.turn":
			if turn.SpanID == "" {
				turn = r
			}
		case "pigo.tool":
			tool = r
		}
	}
	if turn.ParentSpanID != run.SpanID || turn.TraceID != run.TraceID {
		t.Fatalf("turn parent=%s run=%s trace turn=%s run=%s", turn.ParentSpanID, run.SpanID, turn.TraceID, run.TraceID)
	}
	if tool.ParentSpanID != turn.SpanID {
		t.Fatalf("tool parent=%s turn=%s", tool.ParentSpanID, turn.SpanID)
	}
	if tool.Attrs["gen_ai.tool.name"] != "read" {
		t.Fatalf("tool attrs %+v", tool.Attrs)
	}
}
