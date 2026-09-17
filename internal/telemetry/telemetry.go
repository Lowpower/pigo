// Package telemetry is a small OTel-shaped span API with pluggable exporters.
// Call sites use Start/End only; JSONL, OTLP/HTTP JSON, and extension events
// are exporters. The default is noop.
package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/config"
)

// EventSpan is the fire-and-forget extension event carrying a SpanRecord.
const EventSpan = "telemetry_span"

const (
	statusOK    = "ok"
	statusError = "error"
)

// SpanRecord is the stable, exporter-agnostic span payload.
type SpanRecord struct {
	Name              string         `json:"name"`
	TraceID           string         `json:"trace_id"`
	SpanID            string         `json:"span_id"`
	ParentSpanID      string         `json:"parent_span_id,omitempty"`
	StartTimeUnixNano int64          `json:"start_time_unix_nano"`
	EndTimeUnixNano   int64          `json:"end_time_unix_nano"`
	Status            string         `json:"status"`
	Attrs             map[string]any `json:"attrs,omitempty"`
}

// Map returns a JSON object suitable for extension event payloads.
func (r SpanRecord) Map() map[string]any {
	b, err := json.Marshal(r)
	if err != nil {
		return map[string]any{"name": r.Name, "trace_id": r.TraceID, "span_id": r.SpanID}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{"name": r.Name}
	}
	return m
}

// RecordFromMap reconstructs a SpanRecord from an extension payload.
func RecordFromMap(m map[string]any) (SpanRecord, bool) {
	if m == nil {
		return SpanRecord{}, false
	}
	b, err := json.Marshal(m)
	if err != nil {
		return SpanRecord{}, false
	}
	var r SpanRecord
	if err := json.Unmarshal(b, &r); err != nil || r.Name == "" {
		return SpanRecord{}, false
	}
	return r, true
}

// Span is the call-site handle. Safe to End more than once.
type Span interface {
	SetAttribute(k string, v any)
	RecordError(err error)
	End()
}

// Exporter receives finished spans.
type Exporter interface {
	Export(records []SpanRecord)
	Shutdown(ctx context.Context) error
}

type spanKey struct{}

type recSpan struct {
	mu       sync.Mutex
	name     string
	traceID  string
	spanID   string
	parentID string
	start    time.Time
	end      time.Time
	status   string
	attrs    map[string]any
	ended    bool
}

var (
	exportersMu sync.Mutex
	exporters   []Exporter
)

// Reset drops exporters. Tests should call this in cleanup.
func Reset() {
	exportersMu.Lock()
	defer exportersMu.Unlock()
	exporters = nil
}

// AddExporter appends a sink. Ignored if e is nil.
func AddExporter(e Exporter) {
	if e == nil {
		return
	}
	exportersMu.Lock()
	defer exportersMu.Unlock()
	exporters = append(exporters, e)
}

// Allowed reports whether span export and install telemetry may run.
// PIGO_TELEMETRY overrides settings when set (1/true/yes vs 0/false/no).
func Allowed(cfg config.Config) bool {
	if v, ok := os.LookupEnv("PIGO_TELEMETRY"); ok {
		return isTruthyEnvFlag(v)
	}
	return cfg.InstallTelemetryEnabled()
}

func isTruthyEnvFlag(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func jsonlEnabled() bool {
	return isTruthyEnvFlag(os.Getenv("PIGO_TELEMETRY_JSONL"))
}

// Init installs built-in exporters from env when Allowed. Always resets first.
func Init(cfg config.Config) {
	Reset()
	if !Allowed(cfg) {
		return
	}
	if jsonlEnabled() {
		AddExporter(NewJSONLExporter(os.Stderr))
	}
	if TracesEndpoint() != "" {
		AddExporter(NewOTLPExporter())
	}
}

// Shutdown flushes exporters. ctx bounds the wait (2s if ctx has no deadline).
func Shutdown(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	exportersMu.Lock()
	list := append([]Exporter(nil), exporters...)
	exportersMu.Unlock()
	var first error
	for _, e := range list {
		if err := e.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Start begins a child span. The returned context carries the span as parent.
func Start(ctx context.Context, name string) (context.Context, Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	parent, _ := ctx.Value(spanKey{}).(*recSpan)
	s := &recSpan{
		name:   name,
		start:  time.Now(),
		status: statusOK,
		attrs:  map[string]any{},
	}
	if parent != nil {
		s.traceID = parent.traceID
		s.parentID = parent.spanID
	} else {
		s.traceID = newHexID(16)
	}
	s.spanID = newHexID(8)
	return context.WithValue(ctx, spanKey{}, s), s
}

func (s *recSpan) SetAttribute(k string, v any) {
	if s == nil || k == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	if s.attrs == nil {
		s.attrs = map[string]any{}
	}
	s.attrs[k] = v
}

func (s *recSpan) RecordError(err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.status = statusError
}

func (s *recSpan) End() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	s.end = time.Now()
	rec := s.snapshotLocked()
	s.mu.Unlock()
	export(rec)
}

func (s *recSpan) snapshotLocked() SpanRecord {
	attrs := make(map[string]any, len(s.attrs))
	for k, v := range s.attrs {
		attrs[k] = v
	}
	return SpanRecord{
		Name:              s.name,
		TraceID:           s.traceID,
		SpanID:            s.spanID,
		ParentSpanID:      s.parentID,
		StartTimeUnixNano: s.start.UnixNano(),
		EndTimeUnixNano:   s.end.UnixNano(),
		Status:            s.status,
		Attrs:             attrs,
	}
}

func export(rec SpanRecord) {
	exportersMu.Lock()
	list := append([]Exporter(nil), exporters...)
	exportersMu.Unlock()
	for _, e := range list {
		e.Export([]SpanRecord{rec})
	}
}

func newHexID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> (i % 8))
		}
	}
	return hex.EncodeToString(b)
}

type jsonlExporter struct {
	w  io.Writer
	mu sync.Mutex
}

// NewJSONLExporter writes one JSON object per span, newline-delimited.
func NewJSONLExporter(w io.Writer) Exporter {
	if w == nil {
		w = io.Discard
	}
	return &jsonlExporter{w: w}
}

func (j *jsonlExporter) Export(records []SpanRecord) {
	j.mu.Lock()
	defer j.mu.Unlock()
	enc := json.NewEncoder(j.w)
	for _, r := range records {
		_ = enc.Encode(r)
	}
}

func (j *jsonlExporter) Shutdown(context.Context) error { return nil }

type eventExporter struct {
	emit func(event string, payload map[string]any)
}

// NewEventExporter sends each span as EventSpan. emit must not block the agent
// for long; the host uses fire-and-forget frames.
func NewEventExporter(emit func(event string, payload map[string]any)) Exporter {
	return &eventExporter{emit: emit}
}

func (e *eventExporter) Export(records []SpanRecord) {
	if e.emit == nil {
		return
	}
	for _, r := range records {
		e.emit(EventSpan, r.Map())
	}
}

func (e *eventExporter) Shutdown(context.Context) error { return nil }
