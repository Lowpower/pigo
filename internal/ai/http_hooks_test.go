package ai

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Lowpower/pigo/internal/telemetry"
)

func TestApplyExtraHeadersAndTransformBody(t *testing.T) {
	h := make(http.Header)
	h.Set("X-Keep", "1")
	applyExtraHeaders(h, map[string]string{"X-Add": "2", "X-Keep": ""})
	if h.Get("X-Add") != "2" {
		t.Fatalf("X-Add=%q", h.Get("X-Add"))
	}
	if h.Get("X-Keep") != "" {
		t.Fatalf("X-Keep should be deleted, got %q", h.Get("X-Keep"))
	}

	opts := Options{TransformBody: func(body []byte) []byte {
		return append(body, '!')
	}}
	got := transformRequestBody(opts, []byte("hi"))
	if string(got) != "hi!" {
		t.Fatalf("body=%q", got)
	}
	if string(transformRequestBody(Options{}, []byte("hi"))) != "hi" {
		t.Fatal("nil transform should be identity")
	}
}

func TestDoHTTPRecordsProviderSpan(t *testing.T) {
	telemetry.Reset()
	t.Cleanup(telemetry.Reset)
	var buf bytes.Buffer
	telemetry.AddExporter(telemetry.NewJSONLExporter(&buf))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := doHTTP(srv.Client(), req, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	var rec telemetry.SpanRecord
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Name != "pigo.provider.http" {
		t.Fatalf("name %q", rec.Name)
	}
	if rec.Attrs["http.request.method"] != "GET" {
		t.Fatalf("method %+v", rec.Attrs)
	}
	if rec.Attrs["server.address"] != req.URL.Host {
		t.Fatalf("address %+v", rec.Attrs)
	}
	code, ok := rec.Attrs["http.response.status_code"].(float64)
	if !ok {
		// JSON encoder keeps ints as int
		if rec.Attrs["http.response.status_code"] != http.StatusNoContent && rec.Attrs["http.response.status_code"] != 204 {
			t.Fatalf("status %+v", rec.Attrs["http.response.status_code"])
		}
	} else if int(code) != http.StatusNoContent {
		t.Fatalf("status %v", code)
	}
}
