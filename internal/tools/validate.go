package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// argNormalizer adjusts tool arguments before schema checks. edit uses it so
// legacy shapes are normalized before additionalProperties rejects them.
type argNormalizer interface {
	normalizeArgs(map[string]any) map[string]any
}

// prepareToolArgs copies args, coerces string numbers and bools, and checks
// them against the tool schema. The caller's map is left unchanged.
func prepareToolArgs(t Tool, args map[string]any) (map[string]any, error) {
	if n, ok := t.(argNormalizer); ok {
		args = n.normalizeArgs(args)
	}
	cloned, err := cloneArgs(args)
	if err != nil {
		return nil, err
	}
	schema := t.Schema()
	if !isObjectSchema(schema) {
		return cloned, nil
	}
	dropOptionalNulls(cloned, schema)
	coerced := coerceValue(cloned, schema)
	out, ok := coerced.(map[string]any)
	if !ok {
		return nil, errors.New("expected object")
	}
	if err := validateValue(out, schema, ""); err != nil {
		return nil, err
	}
	return out, nil
}

func cloneArgs(args map[string]any) (map[string]any, error) {
	if args == nil {
		return map[string]any{}, nil
	}
	cloned, err := cloneAny(args)
	if err != nil {
		return nil, err
	}
	out, ok := cloned.(map[string]any)
	if !ok || out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func cloneAny(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func isObjectSchema(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	types := schemaTypes(schema)
	if len(types) == 0 {
		_, ok := schema["properties"].(map[string]any)
		return ok
	}
	return contains(types, "object")
}

func dropOptionalNulls(value any, schema map[string]any) {
	if schema == nil {
		return
	}
	if m, ok := value.(map[string]any); ok {
		props, _ := schema["properties"].(map[string]any)
		required := requiredSet(schema)
		for key, raw := range props {
			prop, _ := raw.(map[string]any)
			v, exists := m[key]
			if !exists || prop == nil {
				continue
			}
			if v == nil && !required[key] && !schemaAcceptsNull(prop) {
				delete(m, key)
				continue
			}
			dropOptionalNulls(v, prop)
		}
		if ap, ok := schema["additionalProperties"].(map[string]any); ok {
			for key, v := range m {
				if _, defined := props[key]; defined {
					continue
				}
				dropOptionalNulls(v, ap)
			}
		}
	}
	if arr, ok := value.([]any); ok {
		switch items := schema["items"].(type) {
		case map[string]any:
			for _, item := range arr {
				dropOptionalNulls(item, items)
			}
		case []any:
			for i, item := range arr {
				if i >= len(items) {
					break
				}
				if s, ok := items[i].(map[string]any); ok {
					dropOptionalNulls(item, s)
				}
			}
		}
	}
}

func schemaAcceptsNull(schema map[string]any) bool {
	if schema == nil {
		return true
	}
	for _, typ := range schemaTypes(schema) {
		if typ == "null" {
			return true
		}
	}
	subs := append([]map[string]any{}, schemaList(schema, "anyOf")...)
	subs = append(subs, schemaList(schema, "oneOf")...)
	if len(subs) > 0 {
		for _, sub := range subs {
			if schemaAcceptsNull(sub) {
				return true
			}
		}
		return false
	}
	return len(schemaTypes(schema)) == 0
}

func coerceValue(value any, schema map[string]any) any {
	if schema == nil {
		return value
	}
	if anyOf := schemaList(schema, "anyOf"); len(anyOf) > 0 {
		value = coerceUnion(value, anyOf)
	} else if oneOf := schemaList(schema, "oneOf"); len(oneOf) > 0 {
		value = coerceUnion(value, oneOf)
	} else {
		types := schemaTypes(schema)
		if len(types) > 0 && !matchesAnyType(value, types) {
			for _, typ := range types {
				if next, ok := coercePrimitive(value, typ); ok {
					value = next
					break
				}
			}
		}
	}
	types := schemaTypes(schema)
	if m, ok := value.(map[string]any); ok && (len(types) == 0 || contains(types, "object")) {
		coerceObject(m, schema)
	}
	if arr, ok := value.([]any); ok && (len(types) == 0 || contains(types, "array")) {
		coerceArray(arr, schema)
	}
	return value
}

func coerceUnion(value any, schemas []map[string]any) any {
	for _, schema := range schemas {
		if validateValue(value, schema, "") == nil {
			return value
		}
	}
	for _, schema := range schemas {
		cloned, err := cloneAny(value)
		if err != nil {
			continue
		}
		candidate := coerceValue(cloned, schema)
		if validateValue(candidate, schema, "") == nil {
			return candidate
		}
	}
	return value
}

func coerceObject(m map[string]any, schema map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	for key, raw := range props {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		v, exists := m[key]
		if !exists {
			continue
		}
		m[key] = coerceValue(v, prop)
	}
	ap, ok := schema["additionalProperties"].(map[string]any)
	if !ok {
		return
	}
	for key, v := range m {
		if _, defined := props[key]; defined {
			continue
		}
		m[key] = coerceValue(v, ap)
	}
}

func coerceArray(arr []any, schema map[string]any) {
	switch items := schema["items"].(type) {
	case map[string]any:
		for i, item := range arr {
			arr[i] = coerceValue(item, items)
		}
	case []any:
		for i, item := range arr {
			if i >= len(items) {
				break
			}
			if s, ok := items[i].(map[string]any); ok {
				arr[i] = coerceValue(item, s)
			}
		}
	}
}

func coercePrimitive(value any, typ string) (any, bool) {
	s, ok := value.(string)
	if !ok {
		return nil, false
	}
	switch typ {
	case "integer":
		return parseIntegerString(s)
	case "number":
		return parseNumberString(s)
	case "boolean":
		return parseBoolString(s)
	default:
		return nil, false
	}
}

func parseIntegerString(s string) (int, bool) {
	f, ok := parseFinite(s)
	if !ok || !isFiniteInteger(f) {
		return 0, false
	}
	if f > float64(math.MaxInt) || f < float64(math.MinInt) {
		return 0, false
	}
	return int(f), true
}

func parseNumberString(s string) (float64, bool) {
	return parseFinite(s)
}

func parseFinite(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func parseBoolString(s string) (bool, bool) {
	switch strings.TrimSpace(s) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func isFiniteInteger(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0) && f == math.Trunc(f)
}

func validateValue(value any, schema map[string]any, path string) error {
	if schema == nil {
		return nil
	}
	types := schemaTypes(schema)
	if len(types) > 0 && !matchesAnyType(value, types) {
		return fieldErr(path, "expected "+strings.Join(types, " or "))
	}
	if anyOf := schemaList(schema, "anyOf"); len(anyOf) > 0 && !anyMatch(value, anyOf) {
		return fieldErr(path, "expected "+unionExpected(anyOf))
	}
	if oneOf := schemaList(schema, "oneOf"); len(oneOf) > 0 && countMatches(value, oneOf) != 1 {
		return fieldErr(path, "expected "+unionExpected(oneOf))
	}
	if m, ok := value.(map[string]any); ok && (len(types) == 0 || contains(types, "object")) {
		if err := validateObject(m, schema, path); err != nil {
			return err
		}
	}
	if arr, ok := value.([]any); ok && (len(types) == 0 || contains(types, "array")) {
		if err := validateArray(arr, schema, path); err != nil {
			return err
		}
	}
	return nil
}

func validateObject(m map[string]any, schema map[string]any, path string) error {
	props, _ := schema["properties"].(map[string]any)
	for _, key := range requiredKeys(schema) {
		if _, ok := m[key]; !ok {
			return fieldErr(joinPath(path, key), "required")
		}
	}
	for key, raw := range props {
		v, ok := m[key]
		if !ok {
			continue
		}
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if err := validateValue(v, prop, joinPath(path, key)); err != nil {
			return err
		}
	}
	ap, hasAP := schema["additionalProperties"]
	for key, v := range m {
		if _, defined := props[key]; defined {
			continue
		}
		switch extra := ap.(type) {
		case bool:
			if hasAP && !extra {
				return fmt.Errorf("unexpected field %q", joinPath(path, key))
			}
		case map[string]any:
			if err := validateValue(v, extra, joinPath(path, key)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateArray(arr []any, schema map[string]any, path string) error {
	switch items := schema["items"].(type) {
	case map[string]any:
		for i, item := range arr {
			if err := validateValue(item, items, joinPath(path, strconv.Itoa(i))); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range arr {
			if i >= len(items) {
				break
			}
			s, ok := items[i].(map[string]any)
			if !ok {
				continue
			}
			if err := validateValue(item, s, joinPath(path, strconv.Itoa(i))); err != nil {
				return err
			}
		}
	}
	return nil
}

func anyMatch(value any, schemas []map[string]any) bool {
	return countMatches(value, schemas) > 0
}

func countMatches(value any, schemas []map[string]any) int {
	n := 0
	for _, schema := range schemas {
		if validateValue(value, schema, "") == nil {
			n++
		}
	}
	return n
}

func unionExpected(schemas []map[string]any) string {
	parts := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		types := schemaTypes(schema)
		if len(types) == 0 {
			parts = append(parts, "matching schema")
			continue
		}
		parts = append(parts, strings.Join(types, " or "))
	}
	if len(parts) == 0 {
		return "matching schema"
	}
	return strings.Join(parts, " or ")
}

func schemaTypes(schema map[string]any) []string {
	if schema == nil {
		return nil
	}
	switch t := schema["type"].(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func schemaList(schema map[string]any, key string) []map[string]any {
	if schema == nil {
		return nil
	}
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

func requiredKeys(schema map[string]any) []string {
	if schema == nil {
		return nil
	}
	switch req := schema["required"].(type) {
	case []any:
		out := make([]string, 0, len(req))
		for _, item := range req {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), req...)
	default:
		return nil
	}
}

func requiredSet(schema map[string]any) map[string]bool {
	keys := requiredKeys(schema)
	set := make(map[string]bool, len(keys))
	for _, key := range keys {
		set[key] = true
	}
	return set
}

func matchesAnyType(value any, types []string) bool {
	for _, typ := range types {
		if matchesType(value, typ) {
			return true
		}
	}
	return false
}

func matchesType(value any, typ string) bool {
	switch typ {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "number":
		return isNumber(value)
	case "integer":
		return isIntegerNumber(value)
	default:
		return false
	}
}

func isNumber(v any) bool {
	switch n := v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return !math.IsNaN(float64(n)) && !math.IsInf(float64(n), 0)
	case float64:
		return !math.IsNaN(n) && !math.IsInf(n, 0)
	case json.Number:
		_, err := n.Float64()
		return err == nil
	default:
		return false
	}
}

func isIntegerNumber(v any) bool {
	switch n := v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return isFiniteInteger(float64(n))
	case float64:
		return isFiniteInteger(n)
	case json.Number:
		if _, err := n.Int64(); err == nil {
			return true
		}
		f, err := n.Float64()
		return err == nil && isFiniteInteger(f)
	default:
		return false
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

func fieldErr(path, msg string) error {
	if path == "" {
		return errors.New(msg)
	}
	return fmt.Errorf("%s: %s", path, msg)
}
