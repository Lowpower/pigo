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
	// LLM SDKs (official) — anthropic-sdk-go remains unused (raw HTTP/SSE adapter).
	_ "github.com/anthropics/anthropic-sdk-go"

	// sqlite — optional session backend (not wired yet)
	_ "modernc.org/sqlite"
	// openai-go, google.golang.org/genai, and aws-sdk-go-v2 are imported for real
	// by the provider adapters.
)
