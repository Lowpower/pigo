package ai

import "sort"

// strictToolParameters copies schema and closes it for OpenAI strict function
// calling. Every object gets additionalProperties false and lists all of its
// properties in required. Properties that were optional become nullable. The
// input map is not modified.
func strictToolParameters(schema map[string]any) map[string]any {
	cloned := jsonCloneMap(schema)
	if cloned == nil {
		return schema
	}
	closeStrictSchema(cloned)
	return cloned
}

func closeStrictSchema(schema map[string]any) {
	if schema == nil {
		return
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		for _, sub := range schemaMapList(schema, key) {
			closeStrictSchema(sub)
		}
	}
	switch items := schema["items"].(type) {
	case map[string]any:
		closeStrictSchema(items)
	case []any:
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				closeStrictSchema(m)
			}
		}
	}
	if !isStrictObject(schema) {
		return
	}
	props, _ := schema["properties"].(map[string]any)
	required := strictRequiredSet(schema)
	for key, raw := range props {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		closeStrictSchema(prop)
		if !required[key] {
			makeNullable(prop)
		}
	}
	if len(props) > 0 {
		schema["required"] = strictRequiredKeys(props)
	}
	schema["additionalProperties"] = false
}

func makeNullable(schema map[string]any) {
	if schema == nil || schemaAcceptsNull(schema) {
		return
	}
	if _, ok := schema["anyOf"]; ok {
		appendNullSchema(schema, "anyOf")
		return
	}
	if _, ok := schema["oneOf"]; ok {
		appendNullSchema(schema, "oneOf")
		return
	}
	switch t := schema["type"].(type) {
	case string:
		if t != "" {
			schema["type"] = []any{t, "null"}
		}
	case []any:
		schema["type"] = append(append([]any{}, t...), "null")
	case []string:
		out := make([]any, 0, len(t)+1)
		for _, name := range t {
			out = append(out, name)
		}
		schema["type"] = append(out, "null")
	}
}

func appendNullSchema(schema map[string]any, key string) {
	switch list := schema[key].(type) {
	case []any:
		schema[key] = append(list, map[string]any{"type": "null"})
	case []map[string]any:
		schema[key] = append(list, map[string]any{"type": "null"})
	default:
		schema[key] = []any{map[string]any{"type": "null"}}
	}
}

func schemaAcceptsNull(schema map[string]any) bool {
	if schema == nil {
		return true
	}
	for _, typ := range schemaTypeNames(schema) {
		if typ == "null" {
			return true
		}
	}
	subs := append(schemaMapList(schema, "anyOf"), schemaMapList(schema, "oneOf")...)
	if len(subs) > 0 {
		for _, sub := range subs {
			if schemaAcceptsNull(sub) {
				return true
			}
		}
		return false
	}
	return len(schemaTypeNames(schema)) == 0
}

func isStrictObject(schema map[string]any) bool {
	if _, ok := schema["properties"].(map[string]any); ok {
		return true
	}
	for _, typ := range schemaTypeNames(schema) {
		if typ == "object" {
			return true
		}
	}
	return false
}

func schemaTypeNames(schema map[string]any) []string {
	switch t := schema["type"].(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, v := range t {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), t...)
	default:
		return nil
	}
}

func schemaMapList(schema map[string]any, key string) []map[string]any {
	switch xs := schema[key].(type) {
	case []any:
		out := make([]map[string]any, 0, len(xs))
		for _, item := range xs {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return xs
	default:
		return nil
	}
}

func strictRequiredSet(schema map[string]any) map[string]bool {
	out := map[string]bool{}
	switch req := schema["required"].(type) {
	case []any:
		for _, key := range req {
			if s, ok := key.(string); ok {
				out[s] = true
			}
		}
	case []string:
		for _, key := range req {
			out[key] = true
		}
	}
	return out
}

func strictRequiredKeys(props map[string]any) []any {
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]any, len(keys))
	for i, key := range keys {
		out[i] = key
	}
	return out
}
