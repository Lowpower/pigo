package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
)

func TestNoopStartEndSilent(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var buf bytes.Buffer
	ctx, span := Start(context.Background(), "pigo.turn")
	span.SetAttribute("pigo.turn.index", 0)
	span.End()
	span.End()
	_ = ctx
	if buf.Len() != 0 {
		t.Fatalf("noop wrote %q", buf.String())
	}
}

func TestJSONLExport(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var buf bytes.Buffer
	AddExporter(NewJSONLExporter(&buf))
	ctx, parent := Start(context.Background(), "pigo.run")
	_, child := Start(ctx, "pigo.turn")
	child.SetAttribute("pigo.turn.index", 1)
	child.End()
	parent.End()
	_ = Shutdown(context.Background())

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d want 2\n%s", len(lines), buf.String())
	}
	var turn, run SpanRecord
	if err := json.Unmarshal([]byte(lines[0]), &turn); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &run); err != nil {
		t.Fatal(err)
	}
	if turn.Name != "pigo.turn" || run.Name != "pigo.run" {
		t.Fatalf("names %q %q", turn.Name, run.Name)
	}
	if len(turn.TraceID) != 32 || len(turn.SpanID) != 16 {
		t.Fatalf("ids trace=%q span=%q", turn.TraceID, turn.SpanID)
	}
	if turn.TraceID != run.TraceID {
		t.Fatalf("trace mismatch %s vs %s", turn.TraceID, run.TraceID)
	}
	if turn.ParentSpanID != run.SpanID {
		t.Fatalf("parent %q want %q", turn.ParentSpanID, run.SpanID)
	}
	if turn.Status != "ok" || turn.EndTimeUnixNano < turn.StartTimeUnixNano {
		t.Fatalf("turn record %+v", turn)
	}
}

func TestRecordErrorAndEventExporter(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var got []map[string]any
	AddExporter(NewEventExporter(func(event string, payload map[string]any) {
		if event != EventSpan {
			t.Errorf("event %q", event)
		}
		got = append(got, payload)
	}))
	_, span := Start(context.Background(), "pigo.tool")
	span.SetAttribute("gen_ai.tool.name", "read")
	span.RecordError(errors.New("boom"))
	span.End()
	if len(got) != 1 {
		t.Fatalf("got %d events", len(got))
	}
	rec, ok := RecordFromMap(got[0])
	if !ok || rec.Name != "pigo.tool" || rec.Status != "error" {
		t.Fatalf("record %+v ok=%v", rec, ok)
	}
}

func TestAllowedEnvOverridesSettings(t *testing.T) {
	on := true
	off := false
	cfgOn := config.Config{EnableInstallTelemetry: &on}
	cfgOff := config.Config{EnableInstallTelemetry: &off}

	if err := os.Unsetenv("PIGO_TELEMETRY"); err != nil {
		t.Fatal(err)
	}
	if !Allowed(cfgOn) {
		t.Fatal("settings on should allow")
	}
	if Allowed(cfgOff) {
		t.Fatal("settings off should deny")
	}
	t.Setenv("PIGO_TELEMETRY", "0")
	if Allowed(cfgOn) {
		t.Fatal("PIGO_TELEMETRY=0 should deny")
	}
	t.Setenv("PIGO_TELEMETRY", "true")
	if !Allowed(cfgOff) {
		t.Fatal("PIGO_TELEMETRY=true should allow")
	}
	t.Setenv("PIGO_TELEMETRY", "no")
	if Allowed(cfgOn) {
		t.Fatal("PIGO_TELEMETRY=no should deny")
	}
	if err := os.Unsetenv("PIGO_TELEMETRY"); err != nil {
		t.Fatal(err)
	}
	if !Allowed(config.Config{}) {
		t.Fatal("default settings should allow")
	}
}

func TestOTLPExport(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth %s", r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer tok")
	AddExporter(NewOTLPExporter())
	_, span := Start(context.Background(), "pigo.llm_request")
	span.SetAttribute("gen_ai.request.model", "demo")
	span.End()
	if err := Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("posts = %d", len(bodies))
	}
	var payload map[string]any
	if err := json.Unmarshal(bodies[0], &payload); err != nil {
		t.Fatal(err)
	}
	rs, _ := payload["resourceSpans"].([]any)
	if len(rs) != 1 {
		t.Fatalf("resourceSpans %+v", payload)
	}
	raw := string(bodies[0])
	if !strings.Contains(raw, `"pigo.llm_request"`) || !strings.Contains(raw, `"service.name"`) {
		t.Fatalf("body %s", raw)
	}
}

func TestInitDeniedSkipsOTLP(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("PIGO_TELEMETRY", "0")
	off := false
	Init(config.Config{EnableInstallTelemetry: &off})
	_, span := Start(context.Background(), "pigo.turn")
	span.End()
	_ = Shutdown(context.Background())
	if hits != 0 {
		t.Fatalf("hits = %d", hits)
	}
}

func TestMarshalOTLPAndTracesEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	if got := TracesEndpoint(); got != "http://collector:4318/v1/traces" {
		t.Fatalf("endpoint %s", got)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://collector:4318/v1/traces")
	if got := TracesEndpoint(); got != "http://collector:4318/v1/traces" {
		t.Fatalf("traces endpoint %s", got)
	}
	b, err := MarshalOTLP([]SpanRecord{{
		Name: "pigo.turn", TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("b", 16),
		Status: "ok", Attrs: map[string]any{"pigo.turn.index": 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"resourceSpans"`)) || !bytes.Contains(b, []byte(`"pigo.turn"`)) {
		t.Fatalf("%s", b)
	}
}
