package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/version"
)

// TracesEndpoint is the OTLP/HTTP traces URL, or empty if unset.
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT wins; otherwise ENDPOINT + "/v1/traces".
func TracesEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")); v != "" {
		return v
	}
	base := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if base == "" {
		return ""
	}
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1/traces") {
		return base
	}
	return base + "/v1/traces"
}

// OTLPHeaders parses OTEL_EXPORTER_OTLP_HEADERS and the traces-specific overlay.
func OTLPHeaders() http.Header {
	h := http.Header{}
	parseHeaderEnv(h, os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))
	parseHeaderEnv(h, os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS"))
	return h
}

func parseHeaderEnv(h http.Header, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		h.Set(k, strings.TrimSpace(v))
	}
}

type otlpAttrValue struct {
	StringValue string   `json:"stringValue,omitempty"`
	IntValue    string   `json:"intValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
}

type otlpKeyValue struct {
	Key   string        `json:"key"`
	Value otlpAttrValue `json:"value"`
}

type otlpSpan struct {
	TraceID           string         `json:"traceId"`
	SpanID            string         `json:"spanId"`
	ParentSpanID      string         `json:"parentSpanId,omitempty"`
	Name              string         `json:"name"`
	Kind              int            `json:"kind"`
	StartTimeUnixNano string         `json:"startTimeUnixNano"`
	EndTimeUnixNano   string         `json:"endTimeUnixNano"`
	Attributes        []otlpKeyValue `json:"attributes,omitempty"`
	Status            struct {
		Code int `json:"code"`
	} `json:"status"`
}

type otlpResource struct {
	Attributes []otlpKeyValue `json:"attributes"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

func attrValue(v any) otlpAttrValue {
	switch t := v.(type) {
	case bool:
		b := t
		return otlpAttrValue{BoolValue: &b}
	case int:
		return otlpAttrValue{IntValue: strconv.Itoa(t)}
	case int64:
		return otlpAttrValue{IntValue: strconv.FormatInt(t, 10)}
	case float64:
		x := t
		if x == float64(int64(x)) {
			return otlpAttrValue{IntValue: strconv.FormatInt(int64(x), 10)}
		}
		return otlpAttrValue{DoubleValue: &x}
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return otlpAttrValue{IntValue: strconv.FormatInt(i, 10)}
		}
		if f, err := t.Float64(); err == nil {
			return otlpAttrValue{DoubleValue: &f}
		}
		return otlpAttrValue{StringValue: t.String()}
	case string:
		return otlpAttrValue{StringValue: t}
	default:
		return otlpAttrValue{StringValue: fmt.Sprint(v)}
	}
}

func otlpAttrs(attrs map[string]any) []otlpKeyValue {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]otlpKeyValue, 0, len(attrs))
	for k, v := range attrs {
		out = append(out, otlpKeyValue{Key: k, Value: attrValue(v)})
	}
	return out
}

// MarshalOTLP encodes records as OTLP/HTTP JSON (resourceSpans).
func MarshalOTLP(records []SpanRecord) ([]byte, error) {
	spans := make([]otlpSpan, 0, len(records))
	for _, r := range records {
		sp := otlpSpan{
			TraceID:           r.TraceID,
			SpanID:            r.SpanID,
			ParentSpanID:      r.ParentSpanID,
			Name:              r.Name,
			Kind:              1,
			StartTimeUnixNano: strconv.FormatInt(r.StartTimeUnixNano, 10),
			EndTimeUnixNano:   strconv.FormatInt(r.EndTimeUnixNano, 10),
			Attributes:        otlpAttrs(r.Attrs),
		}
		if r.Status == statusError {
			sp.Status.Code = 2
		} else {
			sp.Status.Code = 1
		}
		spans = append(spans, sp)
	}
	req := otlpRequest{
		ResourceSpans: []otlpResourceSpans{{
			Resource: otlpResource{Attributes: []otlpKeyValue{
				{Key: "service.name", Value: otlpAttrValue{StringValue: "pigo"}},
				{Key: "service.version", Value: otlpAttrValue{StringValue: version.Version}},
			}},
			ScopeSpans: []otlpScopeSpans{{
				Scope: otlpScope{Name: "pigo", Version: version.Version},
				Spans: spans,
			}},
		}},
	}
	return json.Marshal(req)
}

// PostOTLP POSTs records to TracesEndpoint(). No-op if the endpoint is empty.
func PostOTLP(ctx context.Context, records []SpanRecord) error {
	endpoint := TracesEndpoint()
	if endpoint == "" || len(records) == 0 {
		return nil
	}
	body, err := MarshalOTLP(records)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, vs := range OTLPHeaders() {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("otlp http %s", resp.Status)
	}
	return nil
}

type otlpExporter struct {
	wg sync.WaitGroup
}

// NewOTLPExporter POSTs spans as OTLP/HTTP JSON. Export is asynchronous.
func NewOTLPExporter() Exporter {
	return &otlpExporter{}
}

func (o *otlpExporter) Export(records []SpanRecord) {
	if len(records) == 0 || TracesEndpoint() == "" {
		return
	}
	cp := append([]SpanRecord(nil), records...)
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = PostOTLP(ctx, cp)
	}()
}

func (o *otlpExporter) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
