//go:build tools

// Package tools pins optional dependencies in go.mod/go.sum before they are
// imported by production packages.
//
// It is never compiled into the binary: the "tools" build tag excludes it from
// normal builds. It exists only so `go mod tidy` keeps these modules as direct
// dependencies. As each package is actually used in the implementation, its blank
// import here can be removed (the real import then keeps it required).
package tools

import (
	// LLM SDKs (official) — not yet used. The anthropic/openai-completions adapters
	// use raw HTTP/SSE (matching pi); these remain pinned for optional future
	// provider adapters (e.g. openai-responses, google genai, bedrock).
	_ "github.com/anthropics/anthropic-sdk-go"
	_ "github.com/aws/aws-sdk-go-v2/config"
	_ "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	_ "github.com/openai/openai-go"
	_ "google.golang.org/genai"

	// sqlite — optional session backend (not wired yet)
	_ "modernc.org/sqlite"
	// bubbletea/bubbles/lipgloss/glamour, cobra/viper, glob/jsonschema/go-diff
	// are now imported for real by the internal packages.
)
