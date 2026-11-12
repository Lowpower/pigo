package codescript

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Declaration is the text a script sees for one tool: one line for the call
// and its result, then input properties with @ constraints on their comments.
func Declaration(t Tool) string {
	var b strings.Builder
	b.WriteString("await tools.")
	b.WriteString(jsName(t.Name))
	b.WriteString("(args) resolves to ")
	b.WriteString(resultType(t))
	if t.OutputSchema != nil {
		b.WriteString(", including when the call fails")
	}
	b.WriteString(".\n")
	props, _ := t.Schema["properties"].(map[string]any)
	if len(props) == 0 {
		return b.String()
	}
	req := requiredSet(t.Schema)
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		prop, _ := props[name].(map[string]any)
		if tags := constraintTags(prop); tags != "" {
			b.WriteString("/** ")
			b.WriteString(tags)
			b.WriteString(" */\n")
		}
		opt := "?"
		if req[name] {
			opt = ""
		}
		b.WriteString(name)
		b.WriteString(opt)
		b.WriteString(": ")
		b.WriteString(schemaType(prop))
		b.WriteByte('\n')
	}
	return b.String()
}

func resultType(t Tool) string {
	if isMCP(t) {
		structured := "unknown"
		if t.OutputSchema != nil {
			structured = schemaType(t.OutputSchema)
		}
		return "{ content: unknown, structuredContent: " + structured + ", isError: boolean }"
	}
	if t.OutputSchema != nil {
		return schemaType(t.OutputSchema)
	}
	return "string"
}

func isMCP(t Tool) bool {
	return t.Namespace != "" || strings.Contains(t.Name, "mcp__")
}

func constraintTags(prop map[string]any) string {
	if prop == nil {
		return ""
	}
	var tags []string
	if typ, _ := prop["type"].(string); typ == "integer" {
		tags = append(tags, "@integer")
	}
	if n, ok := numText(prop["minimum"]); ok {
		tags = append(tags, "@minimum "+n)
	}
	if n, ok := numText(prop["maximum"]); ok {
		tags = append(tags, "@maximum "+n)
	}
	if p, ok := prop["pattern"].(string); ok && p != "" {
		tags = append(tags, "@pattern "+strings.ReplaceAll(p, "*/", "* /"))
	}
	if d, ok := prop["default"]; ok {
		raw, err := json.Marshal(d)
		if err == nil {
			tags = append(tags, "@default "+string(raw))
		}
	}
	return strings.Join(tags, " ")
}

func schemaType(prop map[string]any) string {
	if prop == nil {
		return "unknown"
	}
	switch typ, _ := prop["type"].(string); typ {
	case "integer", "number", "string", "boolean", "object":
		return typ
	case "array":
		item := "unknown"
		if items, ok := prop["items"].(map[string]any); ok {
			item = schemaType(items)
		}
		return item + "[]"
	default:
		return "unknown"
	}
}

func requiredSet(schema map[string]any) map[string]bool {
	set := map[string]bool{}
	switch req := schema["required"].(type) {
	case []string:
		for _, name := range req {
			set[name] = true
		}
	case []any:
		for _, name := range req {
			if s, ok := name.(string); ok {
				set[s] = true
			}
		}
	}
	return set
}

func numText(v any) (string, bool) {
	switch n := v.(type) {
	case int:
		return strconv.Itoa(n), true
	case int64:
		return strconv.FormatInt(n, 10), true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return "", false
		}
		if n == math.Trunc(n) {
			return strconv.FormatInt(int64(n), 10), true
		}
		return strconv.FormatFloat(n, 'f', -1, 64), true
	case json.Number:
		return n.String(), true
	default:
		return "", false
	}
}
