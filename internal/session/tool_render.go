package session

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Lowpower/pigo/internal/theme"
)

// NewBuiltinToolHTMLRenderer draws grep/find (and other non-template tools) as ANSI HTML.
func NewBuiltinToolHTMLRenderer(th theme.Theme) ToolHTMLRenderer {
	return builtinToolRenderer{th: th}
}

// WithBuiltinToolRenderer fills ToolRenderer from the named TUI theme.
func WithBuiltinToolRenderer(opts HTMLOptions) HTMLOptions {
	if opts.ToolRenderer == nil {
		opts.ToolRenderer = NewBuiltinToolHTMLRenderer(theme.Load(opts.ThemeName, opts.Cwd, opts.AgentDir))
	}
	return opts
}

type builtinToolRenderer struct {
	th theme.Theme
}

func (r builtinToolRenderer) token(name, fallback string) string {
	return themeColor(r.th, name, fallback)
}

func (r builtinToolRenderer) RenderCall(_ string, toolName string, args any) string {
	m, _ := args.(map[string]any)
	var lines []string
	switch toolName {
	case "grep":
		lines = []string{r.formatGrepCall(m)}
	case "find":
		lines = []string{r.formatFindCall(m)}
	default:
		return ""
	}
	return ansiLinesToHTML(trimRenderedLines(lines))
}

func (r builtinToolRenderer) RenderResult(_ string, toolName string, result []map[string]any, _ any, _ bool) (string, string) {
	text := strings.TrimSpace(toolResultText(result))
	var collapsed, expanded int
	switch toolName {
	case "grep":
		collapsed, expanded = 15, -1
	case "find":
		collapsed, expanded = 20, -1
	default:
		return "", ""
	}
	col := ansiLinesToHTML(trimRenderedLines(r.formatOutputLines(text, collapsed)))
	exp := ansiLinesToHTML(trimRenderedLines(r.formatOutputLines(text, expanded)))
	if col == exp {
		return "", exp
	}
	return col, exp
}

func (r builtinToolRenderer) formatGrepCall(args map[string]any) string {
	title := paintBold(r.token("toolTitle", r.th.Tool), "grep")
	pattern, ok := strArg(args, "pattern")
	pat := r.invalidArg()
	if ok {
		pat = paint(r.token("accent", r.th.Accent), "/"+pattern+"/")
	}
	path, pok := strArg(args, "path")
	loc := r.invalidArg()
	if pok {
		if path == "" {
			path = "."
		}
		loc = path
	}
	out := title + " " + pat + paint(r.token("toolOutput", r.th.Assistant), " in "+loc)
	if glob, ok := strArg(args, "glob"); ok && glob != "" {
		out += paint(r.token("toolOutput", r.th.Assistant), " ("+glob+")")
	}
	if n, ok := intArg(args, "limit"); ok {
		out += paint(r.token("toolOutput", r.th.Assistant), " limit "+strconv.Itoa(n))
	}
	return out
}

func (r builtinToolRenderer) formatFindCall(args map[string]any) string {
	title := paintBold(r.token("toolTitle", r.th.Tool), "find")
	pattern, ok := strArg(args, "pattern")
	pat := r.invalidArg()
	if ok {
		pat = paint(r.token("accent", r.th.Accent), pattern)
	}
	path, pok := strArg(args, "path")
	loc := r.invalidArg()
	if pok {
		if path == "" {
			path = "."
		}
		loc = path
	}
	out := title + " " + pat + paint(r.token("toolOutput", r.th.Assistant), " in "+loc)
	if n, ok := intArg(args, "limit"); ok {
		out += paint(r.token("toolOutput", r.th.Assistant), " (limit "+strconv.Itoa(n)+")")
	}
	return out
}

func (r builtinToolRenderer) formatOutputLines(output string, maxLines int) []string {
	if output == "" {
		return nil
	}
	lines := strings.Split(output, "\n")
	if maxLines < 0 || maxLines > len(lines) {
		maxLines = len(lines)
	}
	fg := r.token("toolOutput", r.th.Assistant)
	out := make([]string, 0, maxLines+1)
	out = append(out, "")
	for _, line := range lines[:maxLines] {
		out = append(out, paint(fg, line))
	}
	if remaining := len(lines) - maxLines; remaining > 0 {
		out = append(out, paint(r.token("muted", r.th.Muted), fmt.Sprintf("... (%d more lines)", remaining)))
	}
	return out
}

func (r builtinToolRenderer) invalidArg() string {
	return paint(r.token("error", r.th.Error), "invalid")
}

func strArg(args map[string]any, key string) (string, bool) {
	if args == nil {
		return "", false
	}
	v, ok := args[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func intArg(args map[string]any, key string) (int, bool) {
	if args == nil {
		return 0, false
	}
	switch v := args[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}
