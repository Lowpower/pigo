package session

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/theme"
)

func TestRenderHTMLThemedSessionData(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, dir)
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "<script>hi</script>"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "safe"})

	html, err := RenderHTMLWith(m, HTMLOptions{ThemeName: "dark", SystemPrompt: "you are pigo", Tools: nil})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>hi</script>") {
		t.Fatal("raw script tag leaked into HTML")
	}
	if strings.Contains(html, "{{SESSION_DATA}}") || strings.Contains(html, "{{CSS}}") {
		t.Fatal("unreplaced template placeholders")
	}
	if !strings.Contains(html, "--exportPageBg") {
		t.Fatal("missing theme CSS variables")
	}
	data := decodeSessionData(t, html)
	if data["systemPrompt"] != "you are pigo" {
		t.Fatalf("systemPrompt=%v", data["systemPrompt"])
	}
	entries, _ := data["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("entries=%d", len(entries))
	}
	if !sessionDataContains(data, "hi") {
		t.Fatal("user text missing from SESSION_DATA")
	}
	asst, _ := entries[1].(map[string]any)
	msg, _ := asst["message"].(map[string]any)
	if _, ok := msg["content"].([]any); !ok {
		t.Fatalf("assistant content should be text blocks, got %T", msg["content"])
	}
}

func TestExportHTMLFileOmitsAgentState(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, dir)
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "hello"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "hi"})
	// Flush by writing through Export of in-memory manager first.
	src := filepath.Join(dir, "sess.jsonl")
	if err := os.WriteFile(src, mustJSONL(t, m), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.html")
	if _, err := ExportHTMLFileWith(src, HTMLOptions{OutputPath: out, ThemeName: "light"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	data := decodeSessionData(t, html)
	if _, ok := data["systemPrompt"]; ok {
		t.Fatalf("CLI export should omit systemPrompt: %v", data["systemPrompt"])
	}
	if _, ok := data["tools"]; ok {
		t.Fatalf("CLI export should omit tools: %v", data["tools"])
	}
	if !strings.Contains(html, "--exportPageBg") {
		t.Fatal("light theme missing export vars")
	}
}

func TestRenderHTMLUsesDiskThemeColorTokens(t *testing.T) {
	agent := t.TempDir()
	themes := filepath.Join(agent, "themes")
	if err := os.Mkdir(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
		"name": "mine",
		"vars": {"bg": "#010203"},
		"colors": {
			"accent": "#abcdef",
			"error": "#ff0000",
			"text": "#eeeeee",
			"userMessageBg": "#111111",
			"uniqueExportToken": "#123456"
		},
		"export": {"pageBg": "bg", "cardBg": "#0a0b0c"}
	}`
	if err := os.WriteFile(filepath.Join(themes, "mine.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(t.TempDir(), agent)
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	html, err := RenderHTMLWith(m, HTMLOptions{ThemeName: "mine", AgentDir: agent})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "--uniqueExportToken: #123456") {
		t.Fatalf("missing custom token: %s", html[strings.Index(html, ":root"):strings.Index(html, ":root")+800])
	}
	if !strings.Contains(html, "--accent: #abcdef") {
		t.Fatal("missing accent from disk theme")
	}
	if !strings.Contains(html, "--exportPageBg: #010203") {
		t.Fatal("missing export.pageBg")
	}
}

func TestRenderHTMLPreRendersCustomTools(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	_, _ = m.AppendMessage("assistant", map[string]any{
		"role": "assistant",
		"content": []any{
			map[string]any{"type": "toolCall", "id": "c1", "name": "hello", "arguments": map[string]any{"name": "pigo"}},
		},
	})
	_, _ = m.AppendMessage("toolResult", map[string]any{
		"role": "toolResult", "toolCallId": "c1", "toolName": "hello",
		"content": []any{map[string]any{"type": "text", "text": "Hello, pigo!"}},
	})
	html, err := RenderHTMLWith(m, HTMLOptions{ToolRenderer: stubToolRenderer{}})
	if err != nil {
		t.Fatal(err)
	}
	data := decodeSessionData(t, html)
	rt, _ := data["renderedTools"].(map[string]any)
	got, _ := rt["c1"].(map[string]any)
	if got["callHtml"] != "<div>CALL</div>" || got["resultHtmlExpanded"] != "<div>EXP</div>" {
		t.Fatalf("renderedTools=%v", rt)
	}
}

func TestBuiltinGrepIsPreRenderedButBashIsNot(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	_, _ = m.AppendMessage("assistant", map[string]any{
		"role": "assistant",
		"content": []any{
			map[string]any{"type": "toolCall", "id": "g1", "name": "grep", "arguments": map[string]any{"pattern": "TODO", "path": "."}},
			map[string]any{"type": "toolCall", "id": "b1", "name": "bash", "arguments": map[string]any{"command": "true"}},
		},
	})
	_, _ = m.AppendMessage("toolResult", map[string]any{
		"role": "toolResult", "toolCallId": "g1", "toolName": "grep", "content": "a.go:1:TODO",
	})
	_, _ = m.AppendMessage("toolResult", map[string]any{
		"role": "toolResult", "toolCallId": "b1", "toolName": "bash", "content": "ok",
	})
	html, err := RenderHTMLWith(m, HTMLOptions{ToolRenderer: NewBuiltinToolHTMLRenderer(theme.Load("dark", "", ""))})
	if err != nil {
		t.Fatal(err)
	}
	data := decodeSessionData(t, html)
	rt, _ := data["renderedTools"].(map[string]any)
	if _, ok := rt["b1"]; ok {
		t.Fatalf("bash should stay on template.js, got %v", rt["b1"])
	}
	grep, _ := rt["g1"].(map[string]any)
	if grep == nil || grep["callHtml"] == nil || grep["resultHtmlExpanded"] == nil {
		t.Fatalf("grep missing pre-render: %v", rt)
	}
	if !strings.Contains(fmt.Sprint(grep["callHtml"]), "grep") {
		t.Fatalf("callHtml=%v", grep["callHtml"])
	}
}

func TestCLIExportOmitsRenderedToolsWithoutRenderer(t *testing.T) {
	dir := t.TempDir()
	m := New(t.TempDir(), dir)
	_, _ = m.AppendMessage("assistant", map[string]any{
		"role": "assistant",
		"content": []any{
			map[string]any{"type": "toolCall", "id": "g1", "name": "grep", "arguments": map[string]any{"pattern": "x"}},
		},
	})
	src := filepath.Join(dir, "sess.jsonl")
	if err := os.WriteFile(src, mustJSONL(t, m), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.html")
	if _, err := ExportHTMLFileWith(src, HTMLOptions{OutputPath: out}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	data := decodeSessionData(t, string(b))
	if _, ok := data["renderedTools"]; ok {
		t.Fatalf("CLI file export should omit renderedTools: %v", data["renderedTools"])
	}
}

type stubToolRenderer struct{}

func (stubToolRenderer) RenderCall(string, string, any) string { return "<div>CALL</div>" }
func (stubToolRenderer) RenderResult(string, string, []map[string]any, any, bool) (string, string) {
	return "<div>COL</div>", "<div>EXP</div>"
}

func TestFormatTreeAndExportHTML(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, dir)
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "<script>hi</script>"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "safe"})
	dump := m.FormatTree()
	if !strings.Contains(dump, "script") {
		t.Fatalf("tree dump = %s", dump)
	}
	html, err := RenderHTML(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>hi</script>") {
		t.Fatal("html did not keep script out of markup")
	}
	data := decodeSessionData(t, html)
	if !sessionDataContains(data, "hi") {
		t.Fatal("session data missing user text")
	}
}

func decodeSessionData(t *testing.T, html string) map[string]any {
	t.Helper()
	const mark = `<script id="session-data" type="application/json">`
	i := strings.Index(html, mark)
	if i < 0 {
		t.Fatal("missing session-data script")
	}
	rest := html[i+len(mark):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		t.Fatal("unterminated session-data")
	}
	b64 := strings.TrimSpace(rest[:j])
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func sessionDataContains(data map[string]any, needle string) bool {
	b, err := json.Marshal(data)
	if err != nil {
		return false
	}
	var plain any
	if err := json.Unmarshal(b, &plain); err != nil {
		return false
	}
	return dumpContains(plain, needle)
}

func dumpContains(v any, needle string) bool {
	switch x := v.(type) {
	case string:
		return strings.Contains(x, needle)
	case map[string]any:
		for _, vv := range x {
			if dumpContains(vv, needle) {
				return true
			}
		}
	case []any:
		for _, vv := range x {
			if dumpContains(vv, needle) {
				return true
			}
		}
	}
	return false
}

func mustJSONL(t *testing.T, m *Manager) []byte {
	t.Helper()
	var b strings.Builder
	hb, err := json.Marshal(m.header)
	if err != nil {
		t.Fatal(err)
	}
	b.Write(hb)
	b.WriteByte('\n')
	for _, e := range m.Entries() {
		eb, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(eb)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
