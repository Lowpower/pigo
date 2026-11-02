package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// This file parses the (possibly incomplete) JSON that arrives as streamed
// tool-call argument deltas, always yielding a valid object.

var validJSONEscapes = map[byte]bool{
	'"': true, '\\': true, '/': true, 'b': true, 'f': true,
	'n': true, 'r': true, 't': true,
}

func isControlChar(c byte) bool { return c <= 0x1f }

func escapeControlChar(c byte) string {
	switch c {
	case '\b':
		return "\\b"
	case '\f':
		return "\\f"
	case '\n':
		return "\\n"
	case '\r':
		return "\\r"
	case '\t':
		return "\\t"
	default:
		return fmt.Sprintf("\\u%04x", c)
	}
}

// repairJSON escapes raw control characters inside strings and doubles
// backslashes before invalid escape characters.
func repairJSON(s string) string {
	var b strings.Builder
	inString := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if !inString {
			b.WriteByte(c)
			if c == '"' {
				inString = true
			}
			continue
		}

		if c == '"' {
			b.WriteByte(c)
			inString = false
			continue
		}

		if c == '\\' {
			if i+1 >= len(s) {
				b.WriteString("\\\\")
				continue
			}
			next := s[i+1]

			if next == 'u' && i+6 <= len(s) && isHex4(s[i+2:i+6]) {
				b.WriteString("\\u")
				b.WriteString(s[i+2 : i+6])
				i += 5
				continue
			}

			if validJSONEscapes[next] {
				b.WriteByte('\\')
				b.WriteByte(next)
				i++
				continue
			}

			b.WriteString("\\\\")
			continue
		}

		if isControlChar(c) {
			b.WriteString(escapeControlChar(c))
		} else {
			b.WriteByte(c)
		}
	}

	return b.String()
}

func isHex4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

// parseJSONWithRepair parses json, retrying once with control-character/escape
// repair on failure.
func parseJSONWithRepair(s string, out any) error {
	if err := json.Unmarshal([]byte(s), out); err == nil {
		return nil
	}
	repaired := repairJSON(s)
	if repaired != s {
		return json.Unmarshal([]byte(repaired), out)
	}
	return json.Unmarshal([]byte(s), out) // return the original error
}

// parseToolCallArguments parses a finished tool call's argument text.
// It repairs string escapes, and it does not keep a partial prefix: text that
// is not a JSON object comes back as an empty object plus an error that
// includes the original text.
func parseToolCallArguments(raw string) (map[string]any, string) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, ""
	}
	var parsed any
	if err := parseJSONWithRepair(raw, &parsed); err != nil {
		return map[string]any{}, toolArgumentsError(err, raw)
	}
	obj, ok := parsed.(map[string]any)
	if !ok || obj == nil {
		return map[string]any{}, toolArgumentsError(errors.New("Tool arguments must be a JSON object"), raw)
	}
	return obj, ""
}

func toolArgumentsError(err error, raw string) string {
	return err.Error() + "\n\nReceived arguments:\n" + raw
}

// finalizeToolArguments replaces a streamed preview with a strict parse of the
// accumulated argument text. Blank text keeps arguments a provider already set
// from a structured object.
func finalizeToolArguments(c *Content) {
	if c == nil {
		return
	}
	raw := c.partialJSON
	c.partialJSON = ""
	if strings.TrimSpace(raw) == "" {
		return
	}
	args, errText := parseToolCallArguments(raw)
	c.Arguments = args
	c.argumentsError = errText
}

// parseStreamingJSON parses potentially-incomplete streamed JSON, always
// returning a valid object (empty on total failure). Completion of open
// strings/containers is implemented here (completePartialJSON) with a
// trailing-token trim so a value like {"path":"src/fo is completed to
// {"path":"src/fo"}.
func parseStreamingJSON(partial string) map[string]any {
	if strings.TrimSpace(partial) == "" {
		return map[string]any{}
	}

	var out map[string]any
	if err := parseJSONWithRepair(partial, &out); err == nil && out != nil {
		return out
	}

	if v, ok := tryComplete(partial); ok {
		return v
	}
	if v, ok := tryComplete(repairJSON(partial)); ok {
		return v
	}
	return map[string]any{}
}

// tryComplete trims the string from the end until closing open strings and
// containers yields valid JSON object. Returns false if nothing parses.
func tryComplete(s string) (map[string]any, bool) {
	for end := len(s); end > 0; end-- {
		candidate := completePartialJSON(s[:end])
		var out map[string]any
		if err := json.Unmarshal([]byte(candidate), &out); err == nil && out != nil {
			return out, true
		}
	}
	return nil, false
}

// completePartialJSON closes an open string (if any) and any open objects/arrays,
// so a truncated JSON fragment becomes syntactically complete. It does not fix
// trailing separators; tryComplete handles those by trimming.
func completePartialJSON(s string) string {
	var stack []byte
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	var b strings.Builder
	b.WriteString(s)
	if inString {
		b.WriteByte('"')
	}
	for i := len(stack) - 1; i >= 0; i-- {
		b.WriteByte(stack[i])
	}
	return b.String()
}
