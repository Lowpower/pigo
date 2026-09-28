package bugreport

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/version"
)

func TestParseArgs(t *testing.T) {
	got, err := ParseArgs("editor froze --transcript after save")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Transcript || got.Summary || got.Hint != "editor froze after save" {
		t.Fatalf("%+v", got)
	}
	if _, err := ParseArgs("--transcript --summary boom"); err == nil {
		t.Fatal("both flags should fail")
	}
	if _, err := ParseArgs("--nope"); err == nil {
		t.Fatal("unknown flag should fail")
	}
	got, err = ParseArgs("-- --transcript stays text")
	if err != nil || got.Transcript || got.Hint != "--transcript stays text" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestBuildRedactsAndOmitsTranscript(t *testing.T) {
	t.Setenv("PIGO_SECRET_TOKEN", "sk-should-not-leak")
	header := &session.Header{Type: "session", Version: 3, ID: "sess", Cwd: "/secret/project", Timestamp: "2026-01-01T00:00:00.000Z"}
	parent := "p"
	branch := []session.Entry{
		{
			Type: "message", ID: "a1", ParentID: nil, Timestamp: "2026-01-01T00:00:01.000Z",
			Message: json.RawMessage(`{"role":"assistant","provider":"anthropic","model":"m","stopReason":"error","errorMessage":"bad sk-abcdefghij key","content":[{"type":"text","text":"secret body"}]}`),
		},
		{
			Type: "message", ID: "a2", ParentID: &parent, Timestamp: "2026-01-01T00:00:02.000Z",
			Message: json.RawMessage(`{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"ok"}]}`),
		},
	}
	stack := "goroutine 1\n\t/tmp/ext/main.go:10"
	in := Input{
		Hint:            "it broke",
		IncludeSession:  false,
		SessionID:       "sess",
		MessageCount:    2,
		ThinkingLevel:   "off",
		Model:           &ModelInfo{Provider: "anthropic", ID: "m", BaseURL: "https://user:sk-abcdefghij@example.test/v1?api_key=sekret"},
		Provider:        &ProviderInfo{ID: "anthropic", BaseURL: "https://example.test", HeaderNames: []string{"Authorization"}, AuthTypes: []string{"api_key"}},
		GlobalSettings:  json.RawMessage(`{"theme":"dark","apiKey":"sk-abcdefghij","trackingId":"abc","note":"Bearer abcdefghijklmnop"}`),
		ProjectSettings: json.RawMessage(`{"apiKey":"project-secret"}`),
		IncludeProject:  false,
		Header:          header,
		Branch:          branch,
		Crashes: []CrashRecord{{
			Timestamp: "2026-01-01T00:00:00.000Z", Version: "v", Kind: KindFatal,
			Message: "panic sk-abcdefghij", Stack: &stack, Cwd: "/tmp", Notified: true,
		}},
		ID:  "id-1",
		Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	b, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b.Metadata)
	text := string(raw) + b.SessionJSONL
	for _, leak := range []string{"sk-abcdefghij", "sekret", "project-secret", "abcdefghijklmnop", "abc", "PIGO_SECRET_TOKEN=sk-should-not-leak", "/secret/project"} {
		if strings.Contains(text, leak) {
			t.Fatalf("leaked %q in %s", leak, text)
		}
	}
	if b.Metadata.Session.Cwd != "" || b.Metadata.Session.Included || b.SessionJSONL != "" {
		t.Fatalf("transcript leaked: %+v %q", b.Metadata.Session, b.SessionJSONL)
	}
	if !strings.Contains(text, "PIGO_SECRET_TOKEN") {
		t.Fatal("env name missing")
	}
	if b.Metadata.Environment.Version != version.Version {
		t.Fatalf("version %q", b.Metadata.Environment.Version)
	}
	if len(b.Diagnostics.Assistant) != 1 || strings.Contains(b.Diagnostics.Assistant[0].ErrorMessage, "sk-") {
		t.Fatalf("diag %+v", b.Diagnostics.Assistant)
	}
	if strings.Contains(string(mustJSON(b.Diagnostics)), "secret body") {
		t.Fatal("assistant content included")
	}
	if b.Diagnostics.Crashes[0].Notified {
		t.Fatal("notified should be omitted from the report copy")
	}
	if b.Metadata.Model.BaseURL == "" || strings.Contains(b.Metadata.Model.BaseURL, "user:") {
		t.Fatalf("baseURL %q", b.Metadata.Model.BaseURL)
	}
}

func TestWriteZipIncludesTranscript(t *testing.T) {
	dir := t.TempDir()
	header := &session.Header{Type: "session", Version: 3, ID: "sess", Cwd: "/work", Timestamp: "2026-01-01T00:00:00.000Z"}
	branch := []session.Entry{{
		Type: "message", ID: "u1", Timestamp: "2026-01-01T00:00:01.000Z",
		Message: json.RawMessage(`{"role":"user","content":"see sk-abcdefghij"}`),
	}}
	w, err := Write(dir, Input{
		IncludeSession: true,
		Summary:        "model summary",
		SessionID:      "sess",
		MessageCount:   1,
		Header:         header,
		Branch:         branch,
		ID:             "rep-1",
		Now:            time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(w.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	names := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		buf, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		names[f.Name] = string(buf)
	}
	if _, ok := names["report.json"]; !ok {
		t.Fatalf("names %v", names)
	}
	if _, ok := names["diagnostics.json"]; !ok {
		t.Fatal("missing diagnostics")
	}
	body := names["session.jsonl"]
	if !strings.Contains(body, "/work") || strings.Contains(body, "sk-abcdefghij") {
		t.Fatalf("session jsonl: %s", body)
	}
	if !strings.Contains(names["summary.md"], "model summary") {
		t.Fatalf("summary %q", names["summary.md"])
	}
	if w.CrashCount != 0 {
		t.Fatalf("crashes %d", w.CrashCount)
	}
	data := SessionData(w, "hint", true, true)
	if data["delivery"] != "zip" || data["path"] != w.Path || data["hint"] != "hint" {
		t.Fatalf("%v", data)
	}
	u := IssueURL(w.ID, w.Path, "hint")
	if !strings.Contains(u, "https://github.com/Lowpower/pigo/issues/new?") || strings.Contains(u, "sk-") {
		t.Fatalf("url %s", u)
	}
	info, err := os.Stat(w.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

func TestShouldSuggestBug(t *testing.T) {
	if ShouldSuggestBug(&ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: "429 rate limit"}) {
		t.Fatal("retryable")
	}
	if ShouldSuggestBug(&ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: "request was cancelled"}) {
		t.Fatal("cancelled")
	}
	if ShouldSuggestBug(&ai.AssistantMessage{StopReason: ai.StopAborted, ErrorMessage: "aborted"}) {
		t.Fatal("aborted stop")
	}
	if !ShouldSuggestBug(&ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: "invalid json from provider"}) {
		t.Fatal("want hint")
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
