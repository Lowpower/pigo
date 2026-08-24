//go:build tools

// Package tools pins the planned dependency stack (see docs/migration-plan.md §2)
// so the versions are locked in go.mod/go.sum before the corresponding code exists.
//
// It is never compiled into the binary: the "tools" build tag excludes it from
// normal builds. It exists only so `go mod tidy` keeps these modules as direct
// dependencies. As each package is actually used in the implementation, its blank
// import here can be removed (the real import then keeps it required).
package tools

import (
	// TUI (glamour not used until Phase 5; bubbletea/bubbles/lipgloss are now
	// imported for real by internal/tui, cobra/viper by cmd/pi + internal/config)
	_ "github.com/charmbracelet/glamour"

	// LLM SDKs (official)
	_ "github.com/anthropics/anthropic-sdk-go"
	_ "github.com/aws/aws-sdk-go-v2/config"
	_ "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	_ "github.com/openai/openai-go"
	_ "google.golang.org/genai"

	// Schema, diff, sqlite, glob
	_ "github.com/gobwas/glob"
	_ "github.com/invopop/jsonschema"
	_ "github.com/sergi/go-diff/diffmatchpatch"
	_ "modernc.org/sqlite"
)
