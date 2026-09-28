// Package bugreport builds a local /bug zip: environment, redacted settings,
// assistant error diagnostics, and optional transcript or model summary.
package bugreport

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/version"
)

// SchemaVersion is the report.json / diagnostics.json schema.
const SchemaVersion = 1

// CustomType is the session custom entry written after a report is saved.
const CustomType = "pigo.bug-report"

// Input is the caller-supplied snapshot. Environment is collected at build time.
type Input struct {
	Hint            string
	IncludeSession  bool
	Summary         string
	SessionID       string
	MessageCount    int
	ThinkingLevel   string
	Model           *ModelInfo
	Provider        *ProviderInfo
	Extensions      []Extension
	GlobalSettings  json.RawMessage
	ProjectSettings json.RawMessage
	IncludeProject  bool
	Header          *session.Header
	Branch          []session.Entry
	Crashes         []CrashRecord
	ID              string
	Now             time.Time
}

// ModelInfo is the current model, without credentials.
type ModelInfo struct {
	Provider      string   `json:"provider,omitempty"`
	ID            string   `json:"id,omitempty"`
	Name          string   `json:"name,omitempty"`
	API           string   `json:"api,omitempty"`
	BaseURL       string   `json:"baseUrl,omitempty"`
	Reasoning     bool     `json:"reasoning"`
	Input         []string `json:"input,omitempty"`
	ContextWindow int      `json:"contextWindow,omitempty"`
	MaxTokens     int      `json:"maxTokens,omitempty"`
}

// ProviderInfo is the current provider, without credentials.
type ProviderInfo struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name,omitempty"`
	BaseURL               string   `json:"baseUrl,omitempty"`
	HeaderNames           []string `json:"headerNames"`
	AuthTypes             []string `json:"authTypes"`
	AuthStatus            string   `json:"authStatus,omitempty"`
	UsingOAuth            bool     `json:"usingOAuth"`
	RegisteredByExtension bool     `json:"registeredByExtension"`
}

// Metadata is report.json.
type Metadata struct {
	SchemaVersion int           `json:"schemaVersion"`
	ID            string        `json:"id"`
	CreatedAt     string        `json:"createdAt"`
	Hint          *string       `json:"hint"`
	Environment   Environment   `json:"environment"`
	Session       SessionMeta   `json:"session"`
	Model         *ModelInfo    `json:"model"`
	Provider      *ProviderInfo `json:"provider"`
	ThinkingLevel string        `json:"thinkingLevel,omitempty"`
	Extensions    []Extension   `json:"extensions"`
	Settings      SettingsMeta  `json:"settings"`
}

// Environment is the process snapshot. PIGO_ variables are names only.
type Environment struct {
	Version                  string   `json:"version"`
	GoVersion                string   `json:"goVersion"`
	Platform                 string   `json:"platform"`
	Arch                     string   `json:"arch"`
	OSRelease                string   `json:"osRelease,omitempty"`
	Shell                    string   `json:"shell,omitempty"`
	Terminal                 Terminal `json:"terminal"`
	PigoEnvironmentVariables []string `json:"pigoEnvironmentVariables"`
}

// Terminal is the terminal identity, without values that are not names.
type Terminal struct {
	Term           string `json:"term,omitempty"`
	Program        string `json:"program,omitempty"`
	ProgramVersion string `json:"programVersion,omitempty"`
	Colorterm      string `json:"colorterm,omitempty"`
	Tmux           bool   `json:"tmux"`
	SSH            bool   `json:"ssh"`
	CI             bool   `json:"ci"`
}

// SessionMeta describes the session. Cwd is set only when the transcript is included.
type SessionMeta struct {
	ID              string `json:"id,omitempty"`
	Included        bool   `json:"included"`
	SummaryIncluded bool   `json:"summaryIncluded"`
	MessageCount    int    `json:"messageCount"`
	Cwd             string `json:"cwd,omitempty"`
}

// SettingsMeta is redacted settings JSON. Missing files are null.
type SettingsMeta struct {
	Global  any `json:"global"`
	Project any `json:"project"`
}

// AssistantDiag is one failed assistant turn, without conversation content.
type AssistantDiag struct {
	EntryID       string `json:"entryId"`
	Timestamp     string `json:"timestamp"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	StopReason    string `json:"stopReason,omitempty"`
	RawStopReason string `json:"rawStopReason,omitempty"`
	ErrorMessage  string `json:"errorMessage,omitempty"`
}

// Diagnostics is diagnostics.json.
type Diagnostics struct {
	SchemaVersion         int             `json:"schemaVersion"`
	SessionID             string          `json:"sessionId,omitempty"`
	EntryCount            int             `json:"entryCount"`
	AssistantMessageCount int             `json:"assistantMessageCount"`
	Assistant             []AssistantDiag `json:"assistant"`
	Crashes               []CrashRecord   `json:"crashes"`
}

// Bundle is the zip payload before it is written.
type Bundle struct {
	Metadata     Metadata
	Diagnostics  Diagnostics
	SessionJSONL string
	Summary      string
}

// Written is a zip that landed on disk.
type Written struct {
	ID         string
	CreatedAt  string
	Path       string
	URL        string
	CrashCount int
	Metadata   Metadata
}

// ArchiveName is the zip file name for a report id.
func ArchiveName(id string) string {
	return "pigo-bug-report-" + id + ".zip"
}

// Build assembles the report. It does not touch the filesystem except to read
// the OS release string.
func Build(in Input) (Bundle, error) {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	id := in.ID
	if id == "" {
		id = newID()
	}
	created := now.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	var hint *string
	if strings.TrimSpace(in.Hint) != "" {
		h := strings.TrimSpace(in.Hint)
		hint = &h
	}
	exts := make([]Extension, 0, len(in.Extensions))
	for _, ext := range in.Extensions {
		ext.Source = redactString(ext.Source)
		ext.Path = redactString(ext.Path)
		exts = append(exts, ext)
	}
	meta := Metadata{
		SchemaVersion: SchemaVersion,
		ID:            id,
		CreatedAt:     created,
		Hint:          hint,
		Environment:   collectEnvironment(),
		Session: SessionMeta{
			ID:              in.SessionID,
			Included:        in.IncludeSession,
			SummaryIncluded: strings.TrimSpace(in.Summary) != "",
			MessageCount:    in.MessageCount,
		},
		Model:         redactModel(in.Model),
		Provider:      redactProvider(in.Provider),
		ThinkingLevel: in.ThinkingLevel,
		Extensions:    exts,
		Settings: SettingsMeta{
			Global:  redactRaw(in.GlobalSettings),
			Project: nil,
		},
	}
	if in.IncludeProject {
		meta.Settings.Project = redactRaw(in.ProjectSettings)
	}
	if in.IncludeSession && in.Header != nil {
		meta.Session.Cwd = in.Header.Cwd
	}
	diag := collectDiagnostics(in.SessionID, in.Branch, in.Crashes)
	var sessionJSONL string
	if in.IncludeSession {
		sessionJSONL = redactString(serializeBranch(in.Header, in.Branch))
	}
	summary := ""
	if strings.TrimSpace(in.Summary) != "" {
		summary = strings.TrimSpace(in.Summary)
		if !strings.HasSuffix(summary, "\n") {
			summary += "\n"
		}
	}
	return Bundle{Metadata: meta, Diagnostics: diag, SessionJSONL: sessionJSONL, Summary: summary}, nil
}

// Write builds the bundle and writes it under dir.
func Write(dir string, in Input) (Written, error) {
	bundle, err := Build(in)
	if err != nil {
		return Written{}, err
	}
	if dir == "" {
		dir = "."
	}
	files, err := bundleFiles(bundle)
	if err != nil {
		return Written{}, err
	}
	path := filepath.Join(dir, ArchiveName(bundle.Metadata.ID))
	if err := writeArchive(path, files); err != nil {
		return Written{}, err
	}
	return Written{
		ID:         bundle.Metadata.ID,
		CreatedAt:  bundle.Metadata.CreatedAt,
		Path:       path,
		URL:        IssueURL(bundle.Metadata.ID, path, in.Hint),
		CrashCount: len(bundle.Diagnostics.Crashes),
		Metadata:   bundle.Metadata,
	}, nil
}

// SessionData is the pigo.bug-report custom entry payload.
func SessionData(w Written, hint string, sessionIncluded, summaryIncluded bool) map[string]any {
	data := map[string]any{
		"id":              w.ID,
		"createdAt":       w.CreatedAt,
		"sessionIncluded": sessionIncluded,
		"summaryIncluded": summaryIncluded,
		"delivery":        "zip",
		"path":            w.Path,
	}
	if strings.TrimSpace(hint) != "" {
		data["hint"] = strings.TrimSpace(hint)
	}
	return data
}

func bundleFiles(b Bundle) ([]archiveFile, error) {
	meta, err := json.MarshalIndent(b.Metadata, "", "  ")
	if err != nil {
		return nil, err
	}
	diag, err := json.MarshalIndent(b.Diagnostics, "", "  ")
	if err != nil {
		return nil, err
	}
	files := []archiveFile{
		{Name: "report.json", Body: string(meta) + "\n"},
		{Name: "diagnostics.json", Body: string(diag) + "\n"},
	}
	if b.SessionJSONL != "" {
		files = append(files, archiveFile{Name: "session.jsonl", Body: b.SessionJSONL})
	}
	if b.Summary != "" {
		files = append(files, archiveFile{Name: "summary.md", Body: b.Summary})
	}
	return files, nil
}

type archiveFile struct {
	Name string
	Body string
}

func writeArchive(path string, files []archiveFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil && filepath.Dir(path) != "." {
		return err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f.Name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(f.Body)); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

func collectEnvironment() Environment {
	shell := ""
	if s := os.Getenv("SHELL"); s != "" {
		shell = filepath.Base(s)
	}
	names := []string{}
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(k, "PIGO_") {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return Environment{
		Version:   version.Version,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS,
		Arch:      runtime.GOARCH,
		OSRelease: osRelease(),
		Shell:     shell,
		Terminal: Terminal{
			Term:           os.Getenv("TERM"),
			Program:        os.Getenv("TERM_PROGRAM"),
			ProgramVersion: os.Getenv("TERM_PROGRAM_VERSION"),
			Colorterm:      os.Getenv("COLORTERM"),
			Tmux:           os.Getenv("TMUX") != "",
			SSH:            os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "",
			CI:             os.Getenv("CI") != "",
		},
		PigoEnvironmentVariables: names,
	}
}

func osRelease() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func redactModel(m *ModelInfo) *ModelInfo {
	if m == nil {
		return nil
	}
	cp := *m
	cp.BaseURL = redactString(cp.BaseURL)
	return &cp
}

func redactProvider(p *ProviderInfo) *ProviderInfo {
	if p == nil {
		return nil
	}
	cp := *p
	cp.BaseURL = redactString(cp.BaseURL)
	if cp.HeaderNames == nil {
		cp.HeaderNames = []string{}
	}
	if cp.AuthTypes == nil {
		cp.AuthTypes = []string{}
	}
	sort.Strings(cp.HeaderNames)
	return &cp
}

func redactRaw(raw json.RawMessage) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return redactJSON(v)
}

func collectDiagnostics(sessionID string, branch []session.Entry, crashes []CrashRecord) Diagnostics {
	diag := Diagnostics{
		SchemaVersion: SchemaVersion,
		SessionID:     sessionID,
		EntryCount:    len(branch),
		Assistant:     []AssistantDiag{},
		Crashes:       publicCrashes(crashes),
	}
	for _, e := range branch {
		if e.Type != "message" && e.Type != "" {
			continue
		}
		var payload struct {
			Role          string `json:"role"`
			Provider      string `json:"provider"`
			Model         string `json:"model"`
			StopReason    string `json:"stopReason"`
			RawStopReason string `json:"rawStopReason"`
			ErrorMessage  string `json:"errorMessage"`
		}
		if err := json.Unmarshal(e.Message, &payload); err != nil {
			continue
		}
		if payload.Role != "assistant" {
			continue
		}
		diag.AssistantMessageCount++
		if payload.StopReason != "error" && payload.StopReason != "aborted" && payload.ErrorMessage == "" {
			continue
		}
		diag.Assistant = append(diag.Assistant, AssistantDiag{
			EntryID:       e.ID,
			Timestamp:     e.Timestamp,
			Provider:      payload.Provider,
			Model:         payload.Model,
			StopReason:    payload.StopReason,
			RawStopReason: payload.RawStopReason,
			ErrorMessage:  redactString(payload.ErrorMessage),
		})
	}
	return diag
}

func publicCrashes(in []CrashRecord) []CrashRecord {
	if len(in) == 0 {
		return []CrashRecord{}
	}
	out := make([]CrashRecord, 0, len(in))
	for _, rec := range in {
		rec.Notified = false
		rec.Message = redactString(rec.Message)
		if rec.Stack != nil {
			s := redactString(*rec.Stack)
			rec.Stack = &s
		}
		out = append(out, rec)
	}
	return out
}

func serializeBranch(header *session.Header, entries []session.Entry) string {
	var b strings.Builder
	if header != nil {
		if raw, err := json.Marshal(header); err == nil {
			b.Write(raw)
			b.WriteByte('\n')
		}
	}
	for _, e := range entries {
		raw, err := json.Marshal(e)
		if err != nil {
			continue
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

func newID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
