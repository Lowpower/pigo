package session

import (
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// ExportHTML writes a self-contained HTML dump of the session (pi --export /
// /export when the path ends in .html). It does not yet use pi's themed
// export-html template/CSS/JS.
func ExportHTML(m *Manager, outputPath string) (string, error) {
	if m == nil {
		return "", os.ErrInvalid
	}
	if outputPath == "" {
		base := strings.TrimSuffix(filepath.Base(m.file), ".jsonl")
		outputPath = "pi-session-" + base + ".html"
	}
	b, err := RenderHTML(m)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(outputPath, []byte(b), 0o644); err != nil {
		return "", err
	}
	return outputPath, nil
}

// ExportHTMLFile loads a session JSONL and writes HTML.
func ExportHTMLFile(inputPath, outputPath string) (string, error) {
	m, err := Open(inputPath)
	if err != nil {
		return "", err
	}
	return ExportHTML(m, outputPath)
}

// RenderHTML returns the HTML document for m.
func RenderHTML(m *Manager) (string, error) {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<title>pi session `)
	b.WriteString(html.EscapeString(m.id))
	b.WriteString(`</title><style>
body{font-family:ui-sans-serif,system-ui,sans-serif;max-width:52rem;margin:2rem auto;background:#18181e;color:#e8e8e8;padding:0 1rem}
h1{font-size:1.1rem;color:#bbb}
.meta{color:#888;font-size:.85rem;margin-bottom:1.5rem}
.msg{padding:.9rem 1rem;margin:.75rem 0;border-radius:8px;background:#2a2a32;border-left:4px solid #555}
.user{border-left-color:#6c8}
.assistant{border-left-color:#8af}
.toolResult,.tool{border-left-color:#fc6}
.compaction{border-left-color:#a8f;opacity:.9}
.role{font-size:.75rem;text-transform:uppercase;letter-spacing:.04em;color:#aaa;margin-bottom:.4rem}
pre{white-space:pre-wrap;word-break:break-word;margin:0;font-family:ui-monospace,monospace;font-size:.9rem}
</style></head><body>`)
	b.WriteString("<h1>session ")
	b.WriteString(html.EscapeString(m.id))
	b.WriteString("</h1><div class=\"meta\">")
	b.WriteString(html.EscapeString(m.header.Cwd))
	if m.header.Name != "" {
		b.WriteString(" · ")
		b.WriteString(html.EscapeString(m.header.Name))
	}
	b.WriteString("</div>\n")
	for _, e := range m.Entries() {
		role := entryRole(&e)
		class := role
		if class == "" {
			class = e.Type
		}
		b.WriteString(`<div class="msg `)
		b.WriteString(html.EscapeString(class))
		b.WriteString(`"><div class="role">`)
		b.WriteString(html.EscapeString(class))
		b.WriteString(`</div><pre>`)
		body := userText(&e)
		if body == "" {
			body = strings.TrimSpace(string(e.Message))
			var pretty any
			if json.Unmarshal(e.Message, &pretty) == nil {
				if raw, err := json.MarshalIndent(pretty, "", "  "); err == nil {
					body = string(raw)
				}
			}
		}
		b.WriteString(html.EscapeString(body))
		b.WriteString("</pre></div>\n")
	}
	b.WriteString("</body></html>\n")
	return b.String(), nil
}
