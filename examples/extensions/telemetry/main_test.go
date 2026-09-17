package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Lowpower/pigo/internal/telemetry"
)

func TestHandleSpanPostsOTLP(t *testing.T) {
	var mu sync.Mutex
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("path %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = b
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")

	handleSpan(telemetry.SpanRecord{
		Name: "pigo.turn", TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("b", 16),
		Status: "ok", Attrs: map[string]any{"pigo.turn.index": 0},
	}.Map())

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(string(body), `"resourceSpans"`) || !strings.Contains(string(body), `"pigo.turn"`) {
		t.Fatalf("body %s", body)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
}

func TestHandleSpanNoEndpointSilentEnough(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	handleSpan(map[string]any{})
	handleSpan(telemetry.SpanRecord{Name: "pigo.turn"}.Map())
}

func TestTelemetryExtensionBuilds(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "pigo-telemetry")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
}
