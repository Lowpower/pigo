package session

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
)

// HTMLOptions configures themed HTML export.
type HTMLOptions struct {
	OutputPath   string
	ThemeName    string
	Cwd          string
	AgentDir     string
	SystemPrompt string
	Tools        []ai.Tool
}

type htmlSessionData struct {
	Header       Header    `json:"header"`
	Entries      []Entry   `json:"entries"`
	LeafID       *string   `json:"leafId"`
	SystemPrompt string    `json:"systemPrompt,omitempty"`
	Tools        []ai.Tool `json:"tools,omitempty"`
}

// ExportHTML writes a self-contained HTML dump of the session (--export /
// /export when the path ends in .html).
func ExportHTML(m *Manager, outputPath string) (string, error) {
	return ExportHTMLWith(m, HTMLOptions{OutputPath: outputPath})
}

// ExportHTMLWith writes themed HTML using opts.
func ExportHTMLWith(m *Manager, opts HTMLOptions) (string, error) {
	if m == nil {
		return "", os.ErrInvalid
	}
	outputPath := opts.OutputPath
	if outputPath == "" {
		base := strings.TrimSuffix(filepath.Base(m.file), ".jsonl")
		outputPath = "pigo-session-" + base + ".html"
	}
	b, err := RenderHTMLWith(m, opts)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(outputPath, []byte(b), 0o644); err != nil {
		return "", err
	}
	return outputPath, nil
}

// ExportHTMLFile loads a session JSONL and writes HTML (no AgentState).
func ExportHTMLFile(inputPath, outputPath string) (string, error) {
	return ExportHTMLFileWith(inputPath, HTMLOptions{OutputPath: outputPath})
}

// ExportHTMLFileWith loads a session JSONL and writes themed HTML.
func ExportHTMLFileWith(inputPath string, opts HTMLOptions) (string, error) {
	m, err := Open(inputPath)
	if err != nil {
		return "", err
	}
	return ExportHTMLWith(m, opts)
}

// RenderHTML returns the HTML document for m.
func RenderHTML(m *Manager) (string, error) {
	return RenderHTMLWith(m, HTMLOptions{})
}

// RenderHTMLWith returns the themed HTML document for m.
func RenderHTMLWith(m *Manager, opts HTMLOptions) (string, error) {
	if m == nil {
		return "", os.ErrInvalid
	}
	tmpl, err := htmlFS.ReadFile("html/template.html")
	if err != nil {
		return "", err
	}
	cssTmpl, err := htmlFS.ReadFile("html/template.css")
	if err != nil {
		return "", err
	}
	js, err := htmlFS.ReadFile("html/template.js")
	if err != nil {
		return "", err
	}
	marked, err := htmlFS.ReadFile("html/vendor/marked.min.js")
	if err != nil {
		return "", err
	}
	hljs, err := htmlFS.ReadFile("html/vendor/highlight.min.js")
	if err != nil {
		return "", err
	}

	data := htmlSessionData{
		Header:       m.header,
		Entries:      htmlEntries(m.Entries()),
		SystemPrompt: opts.SystemPrompt,
		Tools:        opts.Tools,
	}
	if m.leafID != "" {
		id := m.leafID
		data.LeafID = &id
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	colors := resolveExportColors(opts.ThemeName, opts.Cwd, opts.AgentDir)
	css := string(cssTmpl)
	css = strings.Replace(css, "{{THEME_VARS}}", colors.ThemeVars, 1)
	css = strings.Replace(css, "{{BODY_BG}}", colors.PageBg, 1)
	css = strings.Replace(css, "{{CONTAINER_BG}}", colors.CardBg, 1)
	css = strings.Replace(css, "{{INFO_BG}}", colors.InfoBg, 1)

	html := string(tmpl)
	html = strings.Replace(html, "{{CSS}}", css, 1)
	html = strings.Replace(html, "{{JS}}", string(js), 1)
	html = strings.Replace(html, "{{SESSION_DATA}}", base64.StdEncoding.EncodeToString(raw), 1)
	html = strings.Replace(html, "{{MARKED_JS}}", string(marked), 1)
	html = strings.Replace(html, "{{HIGHLIGHT_JS}}", string(hljs), 1)
	return html, nil
}

func htmlEntries(entries []Entry) []Entry {
	out := make([]Entry, len(entries))
	copy(out, entries)
	for i := range out {
		out[i].Message = normalizeHTMLMessage(out[i].Message)
	}
	return out
}

func normalizeHTMLMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		return raw
	}
	role, _ := msg["role"].(string)
	if role == "assistant" {
		if s, ok := msg["content"].(string); ok {
			msg["content"] = []any{map[string]any{"type": "text", "text": s}}
		}
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return raw
	}
	return b
}
