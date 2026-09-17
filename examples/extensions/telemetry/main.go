// Command telemetry is a sample span sink. It subscribes to telemetry_span
// and POSTs OTLP/HTTP JSON to OTEL_EXPORTER_OTLP_ENDPOINT. With no endpoint it
// writes one JSONL line to stderr so the sample is visibly alive.
//
// Built-in OTLP (the same env on the pigo process) already exports without a
// plugin. Load this extension to try a custom sink, or to copy the pattern.
// If both are on, spans are sent twice; unset the env on the host when
// debugging the plugin, or point the plugin at a different collector.
//
//	go build -o /tmp/pigo-telemetry ./examples/extensions/telemetry
//	pigo -e /tmp/pigo-telemetry -p "hi"
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/telemetry"
)

func main() {
	err := ext.Serve(ext.Handler{
		Name:   "telemetry",
		Events: []string{telemetry.EventSpan},
		OnEvent: func(event string, payload map[string]any) map[string]any {
			if event == telemetry.EventSpan {
				handleSpan(payload)
			}
			return nil
		},
	})
	if err != nil {
		log.Fatalf("telemetry extension: %v", err)
	}
}

func handleSpan(payload map[string]any) {
	rec, ok := telemetry.RecordFromMap(payload)
	if !ok {
		return
	}
	if telemetry.TracesEndpoint() == "" {
		_ = json.NewEncoder(os.Stderr).Encode(rec)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = telemetry.PostOTLP(ctx, []telemetry.SpanRecord{rec})
}
