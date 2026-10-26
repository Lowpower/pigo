package codemode

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const optionsPrefix = "// @options:"

// maxTimeoutMS is the largest delay setTimeout accepts, which bounds timeout_ms.
const maxTimeoutMS = 2_147_483_647

// Options is the object on a script's first line. MaxOutputTokens is accepted
// and ignored here; output capping belongs to a later change.
type Options struct {
	MaxOutputTokens *int
	TimeoutMS       *int
}

// ParseError is a script that was rejected before it ran.
type ParseError struct{ Message string }

func (e *ParseError) Error() string { return e.Message }

// Parse splits an optional first-line "// @options:" object from source.
// The returned code keeps a leading newline in place of the options line so
// later line numbers still match the script as written. An options line that
// is not first fails, and the caller must not start a worker.
func Parse(input string) (string, Options, error) {
	if strings.TrimSpace(input) == "" {
		return "", Options{}, &ParseError{Message: "expected JavaScript source text (non-empty), optionally with a first line // @options: {...}"}
	}
	lines := strings.Split(input, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), optionsPrefix) {
			return "", Options{}, &ParseError{Message: fmt.Sprintf("the %s line must be the first line of the input; found one on line %d", optionsPrefix, i+1)}
		}
	}
	first := strings.TrimRight(lines[0], "\r")
	trimmed := strings.TrimLeft(first, " \t")
	if !strings.HasPrefix(trimmed, optionsPrefix) {
		return input, Options{}, nil
	}
	nl := strings.IndexByte(input, '\n')
	code := ""
	if nl >= 0 {
		code = input[nl:]
	}
	if strings.TrimSpace(code) == "" {
		return "", Options{}, &ParseError{Message: "the @options line must be followed by JavaScript source on subsequent lines"}
	}
	opt, err := parseOptions(strings.TrimSpace(trimmed[len(optionsPrefix):]))
	if err != nil {
		return "", Options{}, err
	}
	return code, opt, nil
}

func parseOptions(directive string) (Options, error) {
	if directive == "" {
		return Options{}, &ParseError{Message: "@options must be a JSON object with supported fields `max_output_tokens` and `timeout_ms`"}
	}
	var probe any
	if err := json.Unmarshal([]byte(directive), &probe); err != nil {
		return Options{}, &ParseError{Message: "@options must be valid JSON with supported fields `max_output_tokens` and `timeout_ms`: " + err.Error()}
	}
	if _, ok := probe.(map[string]any); !ok {
		return Options{}, &ParseError{Message: "@options must be a JSON object with supported fields `max_output_tokens` and `timeout_ms`"}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(directive), &fields); err != nil {
		return Options{}, &ParseError{Message: "@options must be valid JSON with supported fields `max_output_tokens` and `timeout_ms`: " + err.Error()}
	}
	var opt Options
	for key, raw := range fields {
		switch key {
		case "max_output_tokens":
			n, err := nonNegativeInt(raw, "max_output_tokens", int64(maxSafeInteger))
			if err != nil {
				return Options{}, err
			}
			opt.MaxOutputTokens = &n
		case "timeout_ms":
			n, err := nonNegativeInt(raw, "timeout_ms", maxTimeoutMS)
			if err != nil {
				return Options{}, err
			}
			if n == 0 {
				return Options{}, &ParseError{Message: fmt.Sprintf("@options field `timeout_ms` must be a positive integer up to %d", maxTimeoutMS)}
			}
			opt.TimeoutMS = &n
		default:
			return Options{}, &ParseError{Message: fmt.Sprintf("@options only supports `max_output_tokens` and `timeout_ms`; got `%s`", key)}
		}
	}
	return opt, nil
}

// maxSafeInteger is JS Number.MAX_SAFE_INTEGER. max_output_tokens stays within it.
const maxSafeInteger = 9_007_199_254_740_991

func nonNegativeInt(raw json.RawMessage, field string, upper int64) (int, error) {
	var num json.Number
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&num); err != nil || dec.More() {
		return 0, &ParseError{Message: fmt.Sprintf("@options field `%s` must be a non-negative integer", field)}
	}
	text := num.String()
	if strings.ContainsAny(text, ".eE") {
		return 0, &ParseError{Message: fmt.Sprintf("@options field `%s` must be a non-negative integer", field)}
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 0 || n > upper {
		bound := "a non-negative safe integer"
		if field == "timeout_ms" {
			bound = fmt.Sprintf("a positive integer up to %d", maxTimeoutMS)
		}
		return 0, &ParseError{Message: fmt.Sprintf("@options field `%s` must be %s", field, bound)}
	}
	return int(n), nil
}
